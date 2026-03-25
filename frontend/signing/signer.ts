/**
 * Signing operations for reviewflow confirmations.
 * Computes SHA-256 digest and signs with ECDSA P-256 via Web Crypto API.
 */

import { getPrivateKey, getCertificatePEM, importCertificate } from "./keystore"

export interface SignatureResult {
  signature: string    // base64-encoded ECDSA signature
  certificate: string  // PEM certificate
  digest: string       // hex-encoded SHA-256
}

/**
 * Compute the SHA-256 hex digest of raw markdown.
 */
export async function computeDigest(markdown: string): Promise<string> {
  const bytes = new TextEncoder().encode(markdown)
  const hash = await crypto.subtle.digest("SHA-256", bytes)
  const arr = new Uint8Array(hash)
  return Array.from(arr).map(b => b.toString(16).padStart(2, "0")).join("")
}

/**
 * Sign a reviewflow confirmation.
 * Returns the signature, certificate, and digest, or null if no key/cert available.
 */
export async function signConfirmation(
  username: string,
  markdown: string,
): Promise<SignatureResult | null> {
  const privateKey = await getPrivateKey(username)
  if (!privateKey) return null

  let certPEM = await getCertificatePEM(username)
  if (!certPEM) {
    // Certificate not in local store — try fetching from server (admin may have signed it).
    try {
      const resp = await fetch(`/api/plugin/reviewflow/v1/cert/${encodeURIComponent(username)}`)
      if (resp.ok) {
        const data = await resp.json()
        if (data.certificate_pem) {
          await importCertificate(username, data.certificate_pem)
          certPEM = data.certificate_pem
        }
      }
    } catch { /* server unreachable — continue without cert */ }
  }
  if (!certPEM) return null

  // Compute digest (for the server to verify content matches)
  const digest = await computeDigest(markdown)

  // Sign the raw markdown bytes — Web Crypto's ECDSA with hash:"SHA-256"
  // hashes the input internally before signing.
  const markdownBytes = new TextEncoder().encode(markdown)

  const signature = await crypto.subtle.sign(
    { name: "ECDSA", hash: "SHA-256" },
    privateKey,
    markdownBytes,
  )

  const signatureB64 = btoa(String.fromCharCode(...new Uint8Array(signature)))

  return {
    signature: signatureB64,
    certificate: certPEM,
    digest,
  }
}
