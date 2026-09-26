// Tests for frontend/signing/signer.ts.
//
// signer.ts calls fetch(/api/plugin/reviewflow/v1/cert/{user}) to pick
// up the current server-side cert (so a fresh admin-issued cert is
// respected). We stub fetch here so tests are hermetic:
//   - stubFetchReject     — network unreachable, code falls through to
//                            the locally-stored cert.
//   - stubFetchOK(pem)    — server returns a non-revoked cert PEM.
//   - stubFetchRevoked()  — server returns revoked:true, code either
//                            throws (if local PEM matches) or returns
//                            null (fresh key awaiting a new cert).

import "./setup"

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest"
import { generateKeypair, importCertificate, deleteKey, getPublicKeySPKI } from "../../signing/keystore"
import { computeDigest, signConfirmation } from "../../signing/signer"

let fetchSpy: ReturnType<typeof vi.spyOn> | null = null

function stubFetchReject() {
  fetchSpy = vi.spyOn(globalThis, "fetch" as any).mockRejectedValue(new Error("network"))
}
function stubFetchOK(pem: string, revoked = false) {
  fetchSpy = vi.spyOn(globalThis, "fetch" as any).mockResolvedValue({
    ok: true,
    json: async () => ({ certificate_pem: pem, revoked }),
  } as any)
}
function stubFetchRevoked(pem: string) {
  fetchSpy = vi.spyOn(globalThis, "fetch" as any).mockResolvedValue({
    ok: true,
    json: async () => ({ certificate_pem: pem, revoked: true }),
  } as any)
}
function stubFetchNoCert() {
  fetchSpy = vi.spyOn(globalThis, "fetch" as any).mockResolvedValue({
    ok: true,
    json: async () => ({}),
  } as any)
}

beforeEach(() => {
  // Force fetch to exist on the global so vi.spyOn can attach.
  if (typeof (globalThis as any).fetch !== "function") {
    ;(globalThis as any).fetch = async () => {
      throw new Error("network")
    }
  }
})

afterEach(() => {
  fetchSpy?.mockRestore()
  fetchSpy = null
})

async function setupUserWithLocalCert(name: string): Promise<{ pem: string; spkiB64: string }> {
  await generateKeypair(name)
  const spkiB64 = (await getPublicKeySPKI(name))!
  // A minimal PEM; signer.ts only stores/returns it verbatim, it does
  // not parse it. The server's VerifySignature is where an X.509 parse
  // happens — that path is exercised by the backend tests.
  const pem = `-----BEGIN CERTIFICATE-----\n${spkiB64}\n-----END CERTIFICATE-----\n`
  await importCertificate(name, pem)
  return { pem, spkiB64 }
}

describe("computeDigest", () => {
  it("matches the known SHA-256 hex of 'hello world'", async () => {
    // Same expected value the backend test uses — proves the two ends
    // agree on the digest wire format.
    expect(await computeDigest("hello world")).toBe("b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9")
  })

  it("digest of the empty string is the SHA-256 of empty input", async () => {
    expect(await computeDigest("")).toBe("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
  })
})

describe("signConfirmation", () => {
  it("returns null when the user has no key at all", async () => {
    stubFetchReject()
    const res = await signConfirmation("ghost-signer-1", "body")
    expect(res).toBeNull()
  })

  it("returns null when the user has a key but no cert (server unreachable)", async () => {
    await generateKeypair("no-cert-signer")
    stubFetchReject()
    const res = await signConfirmation("no-cert-signer", "body")
    expect(res).toBeNull()
    await deleteKey("no-cert-signer")
  })

  it("returns null when the user has a key but neither local nor server cert (server reachable, no cert)", async () => {
    await generateKeypair("no-cert-server-ok")
    stubFetchNoCert()
    const res = await signConfirmation("no-cert-server-ok", "body")
    expect(res).toBeNull()
    await deleteKey("no-cert-server-ok")
  })

  it("returns a signature verifiable against the stored SPKI when everything is set up (server unreachable → local cert wins)", async () => {
    const name = "signer-happy-1"
    const { pem } = await setupUserWithLocalCert(name)
    stubFetchReject()

    const body = "the page contents\n"
    const res = await signConfirmation(name, body)
    expect(res).not.toBeNull()
    expect(res!.certificate).toBe(pem)
    expect(res!.digest).toBe(await computeDigest(body))

    // Verify the signature cryptographically against the SPKI. This is
    // the wire-level assertion: the browser's Web Crypto output is
    // valid ECDSA-SHA256 that any P-256 verifier — including the Go
    // backend's — will accept.
    const spkiBytes = Uint8Array.from(atob((await getPublicKeySPKI(name))!), (c) => c.charCodeAt(0))
    const pubKey = await crypto.subtle.importKey("spki", spkiBytes, { name: "ECDSA", namedCurve: "P-256" }, true, [
      "verify",
    ])
    const sigBytes = Uint8Array.from(atob(res!.signature), (c) => c.charCodeAt(0))
    const payload = new TextEncoder().encode(body)
    const ok = await crypto.subtle.verify({ name: "ECDSA", hash: "SHA-256" }, pubKey, sigBytes, payload)
    expect(ok).toBe(true)

    await deleteKey(name)
  })

  it("throws when the server says the local PEM is revoked (holding-a-revoked-cert case)", async () => {
    // Precondition: local IndexedDB PEM matches the server's PEM, and
    // the server marks that PEM as revoked. signConfirmation must
    // throw a user-facing error rather than silently returning null,
    // so the caller can show "your cert was revoked, ask admin" in
    // the UI.
    const name = "signer-revoked-holding"
    const { pem } = await setupUserWithLocalCert(name)
    stubFetchRevoked(pem)
    await expect(signConfirmation(name, "body")).rejects.toThrow(/revoked/i)
    await deleteKey(name)
  })

  it("returns null when the server-side cert is revoked but the local cert differs (fresh key awaiting new cert)", async () => {
    // Precondition: user rotated their key (imported a new local PEM),
    // server still knows a stale one and marks it revoked. Silent null
    // return signals "no valid signature available", so the confirm
    // flow falls back to unsigned, rather than surfacing a scary
    // "your cert was revoked" that no longer describes the actual
    // local state.
    const name = "signer-revoked-freshkey"
    await setupUserWithLocalCert(name)
    // A different PEM to represent the "old, revoked" server cert.
    stubFetchRevoked(`-----BEGIN CERTIFICATE-----\nAAAADIFFERENT\n-----END CERTIFICATE-----\n`)
    const res = await signConfirmation(name, "body")
    expect(res).toBeNull()
    await deleteKey(name)
  })

  it("uses the server's fresher PEM over a stale local one when the server is reachable", async () => {
    // Simulate the admin re-issuing the user's cert via the admin form
    // (which historically did NOT push to IndexedDB). Local has the
    // old PEM, server has the new. signConfirmation must return the
    // server's PEM in the result and, as a side effect, mirror it to
    // IndexedDB.
    const name = "signer-server-fresher"
    await setupUserWithLocalCert(name)
    const serverPEM = `-----BEGIN CERTIFICATE-----\nSERVER-NEWER-PEM\n-----END CERTIFICATE-----\n`
    stubFetchOK(serverPEM)

    const res = await signConfirmation(name, "body")
    expect(res).not.toBeNull()
    expect(res!.certificate).toBe(serverPEM)

    // Side effect: IndexedDB now has the server's PEM.
    const { getCertificatePEM } = await import("../../signing/keystore")
    expect(await getCertificatePEM(name)).toBe(serverPEM)

    await deleteKey(name)
  })
})
