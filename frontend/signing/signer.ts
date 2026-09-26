/**
 * Signing operations for reviewflow confirmations.
 * Computes SHA-256 digest and signs with ECDSA P-256 via Web Crypto API.
 */

import { getPrivateKey, getCertificatePEM, importCertificate } from "./keystore"

export interface SignatureResult {
  signature: string // base64-encoded ECDSA signature
  certificate: string // PEM certificate
  digest: string // hex-encoded SHA-256
}

/**
 * Compute the SHA-256 hex digest of raw markdown.
 */
export async function computeDigest(markdown: string): Promise<string> {
  const bytes = new TextEncoder().encode(markdown)
  const hash = await crypto.subtle.digest("SHA-256", bytes)
  const arr = new Uint8Array(hash)
  return Array.from(arr)
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("")
}

/**
 * Sign a reviewflow confirmation.
 * Returns the signature, certificate, and digest, or null if no key/cert available.
 *
 * The server is queried FIRST for the currently-registered cert so an admin
 * who re-issued their own certificate via the admin form (which historically
 * did not update this browser's IndexedDB) still signs with the fresh cert
 * rather than the stale one — cert #1's fingerprint would otherwise trip
 * the revocation guard the very next time they try to sign.
 */
export async function signConfirmation(username: string, markdown: string): Promise<SignatureResult | null> {
  const privateKey = await getPrivateKey(username)
  if (!privateKey) return null

  const localPEM = await getCertificatePEM(username)
  let certPEM: string | null = null
  try {
    const resp = await fetch(`/api/plugin/reviewflow/v1/cert/${encodeURIComponent(username)}`)
    if (resp.ok) {
      const data = await resp.json()
      if (data.certificate_pem) {
        if (data.revoked) {
          // Server says the stored cert is revoked. Two cases:
          //   - Local IndexedDB has the same PEM → user is holding the
          //     revoked cert. Fail loud so they see the reason instead
          //     of rounding through the sign path to a generic error.
          //   - Local IndexedDB has a different PEM (or null) → the
          //     browser has since generated a fresh key that has never
          //     been signed. Silent null return, so the caller falls
          //     back to unsigned confirmation instead of pretending to
          //     sign with a revoked cert.
          if (localPEM && localPEM === data.certificate_pem) {
            throw new Error(
              "Your signing certificate has been revoked. Ask your admin to sign a fresh public key before confirming."
            )
          }
          return null
        }
        certPEM = data.certificate_pem
        if (localPEM !== certPEM) {
          // Server has a newer cert than our IndexedDB — sync so future
          // reads (and offline signing paths) see the same PEM the server
          // is validating against.
          try {
            await importCertificate(username, certPEM!)
          } catch {
            /* best-effort */
          }
        }
      }
    }
  } catch (err) {
    // Re-throw our own revocation error so the caller shows the message
    // instead of silently falling back to whatever's in IndexedDB.
    if (err instanceof Error && err.message.includes("revoked")) throw err
    /* server unreachable — fall through to local copy */
  }

  if (!certPEM) certPEM = localPEM
  if (!certPEM) return null

  // Compute digest (for the server to verify content matches)
  const digest = await computeDigest(markdown)

  // Sign the raw markdown bytes — Web Crypto's ECDSA with hash:"SHA-256"
  // hashes the input internally before signing.
  const markdownBytes = new TextEncoder().encode(markdown)

  const signature = await crypto.subtle.sign({ name: "ECDSA", hash: "SHA-256" }, privateKey, markdownBytes)

  const signatureB64 = btoa(String.fromCharCode(...new Uint8Array(signature)))

  return {
    signature: signatureB64,
    certificate: certPEM,
    digest,
  }
}
