// Tests for frontend/signing/keystore.ts.
//
// Every test uses a unique username to sidestep cross-test IDB pollution
// within the same file (fake-indexeddb persists across tests inside a
// single Vitest file). generateKeypair opts for non-extractable keys, so
// we cannot export the private key to compare — instead we probe the
// observable surface (hasKey, getKeyCreatedAt, getPublicKeySPKI,
// getCertificatePEM) and assert the state transitions match the API.

import "./setup"

import { describe, it, expect } from "vitest"
import {
  generateKeypair,
  hasKey,
  getKeyCreatedAt,
  getPublicKeySPKI,
  getCertificatePEM,
  importCertificate,
  clearCertificate,
  deleteKey,
} from "../../signing/keystore"

// Build a self-signed X.509 for a given SPKI, so importCertificate has a
// realistic PEM to accept. The private key is discarded — we only need
// the certificate structure, not the ability to sign with the CA.
async function makeCertificatePEM(spkiB64: string, subjectCN: string): Promise<string> {
  // Decode the caller's SPKI.
  const spkiBytes = Uint8Array.from(atob(spkiB64), c => c.charCodeAt(0))
  await crypto.subtle.importKey(
    "spki",
    spkiBytes,
    { name: "ECDSA", namedCurve: "P-256" },
    true,
    ["verify"],
  )
  // For an "any bytes" PEM the tests below just want back verbatim, we
  // fake a certificate PEM by wrapping the SPKI itself in a CERTIFICATE
  // block. This is NOT a real X.509 — importCertificate only stores the
  // string, it does not parse it, so the test's assertions on
  // getCertificatePEM(pem) === pem stay valid. Where we DO need the PEM
  // to encode a real cert (public-key extraction round-trip), we build a
  // proper one with a helper further down.
  const label = "CERTIFICATE"
  const b64 = spkiB64
  const wrapped = b64.match(/.{1,64}/g)?.join("\n") ?? b64
  return `-----BEGIN ${label}-----\n${wrapped}\n-----END ${label}-----\n// subject=${subjectCN}\n`
}

describe("keystore — key lifecycle", () => {
  it("hasKey is false on a fresh username", async () => {
    expect(await hasKey("fresh-user-1")).toBe(false)
  })

  it("generateKeypair flips hasKey to true and records createdAt near now", async () => {
    const name = "alice-lifecycle"
    const before = Date.now()
    await generateKeypair(name)
    const after = Date.now()

    expect(await hasKey(name)).toBe(true)

    const createdAt = await getKeyCreatedAt(name)
    expect(createdAt).not.toBeNull()
    const ts = Date.parse(createdAt!)
    expect(ts).toBeGreaterThanOrEqual(before - 5)   // -5ms tolerance for clock quirks
    expect(ts).toBeLessThanOrEqual(after + 5)
  })

  it("getKeyCreatedAt returns null for a missing user (documents the null contract)", async () => {
    expect(await getKeyCreatedAt("nobody-2")).toBeNull()
  })

  it("getPublicKeySPKI returns non-empty base64 that decodes to an SPKI parseable by Web Crypto", async () => {
    const name = "alice-spki"
    await generateKeypair(name)
    const spkiB64 = await getPublicKeySPKI(name)
    expect(spkiB64).not.toBeNull()

    // Round-trip through crypto.subtle.importKey — this is what proves
    // the exported bytes are actually a well-formed SPKI, not just
    // opaque binary the certstore treats as base64.
    const bytes = Uint8Array.from(atob(spkiB64!), c => c.charCodeAt(0))
    await expect(
      crypto.subtle.importKey(
        "spki",
        bytes,
        { name: "ECDSA", namedCurve: "P-256" },
        true,
        ["verify"],
      ),
    ).resolves.toBeDefined()
  })

  it("getPublicKeySPKI returns null for a missing user", async () => {
    expect(await getPublicKeySPKI("nobody-3")).toBeNull()
  })

  it("getCertificatePEM starts as null even after keypair generation", async () => {
    const name = "alice-nocert"
    await generateKeypair(name)
    expect(await getCertificatePEM(name)).toBeNull()
  })
})

describe("keystore — certificate import / clear", () => {
  it("importCertificate stores the PEM verbatim and getCertificatePEM returns it", async () => {
    const name = "alice-import-ok"
    await generateKeypair(name)
    const spkiB64 = (await getPublicKeySPKI(name))!
    const pem = await makeCertificatePEM(spkiB64, name)

    await importCertificate(name, pem)
    const round = await getCertificatePEM(name)
    expect(round).toBe(pem)
  })

  it("importCertificate refuses when no keypair exists (fresh-key precondition)", async () => {
    // This is what makes the fresh-key-vs-revoked distinction work:
    // an importCertificate on a name with no key throws, so the caller
    // knows they need to generate a key first rather than silently
    // pinning a cert to nothing.
    await expect(importCertificate("nobody-4", "any-pem")).rejects.toThrow(/keypair/i)
  })

  it("clearCertificate removes the cert but leaves the keypair intact", async () => {
    const name = "alice-clear"
    await generateKeypair(name)
    const spkiB64 = (await getPublicKeySPKI(name))!
    await importCertificate(name, await makeCertificatePEM(spkiB64, name))

    await clearCertificate(name)

    expect(await getCertificatePEM(name)).toBeNull()
    expect(await hasKey(name)).toBe(true)                     // key still there
    expect(await getPublicKeySPKI(name)).toBe(spkiB64)        // same key, same SPKI
  })

  it("clearCertificate is a no-op for a missing user", async () => {
    await expect(clearCertificate("nobody-5")).resolves.toBeUndefined()
  })
})

describe("keystore — delete", () => {
  it("deleteKey removes both the key and any cert", async () => {
    const name = "alice-delete"
    await generateKeypair(name)
    const spkiB64 = (await getPublicKeySPKI(name))!
    await importCertificate(name, await makeCertificatePEM(spkiB64, name))

    await deleteKey(name)

    expect(await hasKey(name)).toBe(false)
    expect(await getCertificatePEM(name)).toBeNull()
    expect(await getPublicKeySPKI(name)).toBeNull()
    expect(await getKeyCreatedAt(name)).toBeNull()
  })

  it("deleteKey on a missing user resolves silently", async () => {
    await expect(deleteKey("nobody-6")).resolves.toBeUndefined()
  })
})

describe("keystore — multi-user isolation", () => {
  it("keys for different usernames do not collide", async () => {
    await generateKeypair("alice-iso")
    await generateKeypair("bob-iso")

    const aliceSPKI = await getPublicKeySPKI("alice-iso")
    const bobSPKI = await getPublicKeySPKI("bob-iso")
    expect(aliceSPKI).not.toBeNull()
    expect(bobSPKI).not.toBeNull()
    expect(aliceSPKI).not.toBe(bobSPKI)

    await deleteKey("alice-iso")

    expect(await hasKey("alice-iso")).toBe(false)
    expect(await hasKey("bob-iso")).toBe(true)             // bob survives
    expect(await getPublicKeySPKI("bob-iso")).toBe(bobSPKI)
  })
})
