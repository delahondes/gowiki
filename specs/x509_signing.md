# X.509 Document Signing — Specification (Draft)

## 1. Problem Statement

The current reviewflow confirmation is a server-side record: "user X clicked Confirm at time T on version V." This provides accountability (audit trail) but not **non-repudiation** — the system administrator, who controls the server, the database, and the session store, could theoretically forge a confirmation record.

In regulated environments (ISO 13485, IVDR, GxP), auditors may require proof that:
1. A specific person approved a specific version of a document
2. That approval cannot be forged by anyone else, including the system administrator
3. The approval can be independently verified by a third party

Cryptographic signing with X.509 certificates solves this: the signer holds a private key that never leaves their device, and the signature can be verified against their public certificate by anyone.

## 2. Trust Model

### The fundamental constraint

Non-repudiation requires that the **private key is controlled exclusively by the signer**. If anyone else (including the admin) has access to the private key, they can forge signatures, and the scheme provides no more trust than the current click-to-confirm system.

### What the wiki does and does not control

| Component | Controlled by | Notes |
| --- | --- | --- |
| Private key | User only | Generated on user's device, never uploaded |
| Public certificate | Wiki (stored) | Used for verification |
| Identity attestation | CA (or admin) | "This public key belongs to this person" |
| Signed digest | Wiki (stored) | Proof of approval |
| Document content | Wiki | The content that was signed |

### Trust levels

**Level 1 — Self-attestation (minimal)**
- User generates a keypair and uploads the public certificate
- The wiki trusts the certificate because the user is authenticated (logged in)
- Identity proof: "the person who logged in as alice uploaded this cert"
- Strength: private key control is real, but identity binding is only as strong as the login system
- Suitable for: internal use where employees are known

**Level 2 — Admin co-signature (recommended)**
- User generates a keypair on their device
- User presents the public certificate to the admin in person (or via a verified channel)
- Admin signs the user's certificate with a company CA key, creating a signed certificate chain
- The admin attests identity but **cannot forge signatures** (doesn't have the user's private key)
- Strength: identity is verified by a human, signatures are cryptographically independent
- Suitable for: ISO compliance, quality management systems

**Level 3 — External CA (strongest)**
- User obtains a certificate from a trusted third-party CA (Actalis, Sectigo, EIDAS-qualified provider)
- The CA verifies identity through their own process (ID check, email verification, etc.)
- The wiki verifies signatures against the CA's root certificate
- Strength: identity and key binding are both independent of the wiki and its admin
- Suitable for: highly regulated environments, cross-organization trust

### Recommended default: Level 2

For a company wiki used in a quality management context, Level 2 provides the right balance:
- The admin cannot forge signatures (the critical guarantee)
- Identity verification is face-to-face (practical in a company of 10-50 people)
- No external dependency or recurring cost
- An auditor can verify the chain: company CA → user cert → signature on document

## 3. Architecture

### 3.1 Key generation (client-side)

The user generates an RSA or ECDSA keypair **in the browser** using the Web Crypto API. The private key is stored in the browser's IndexedDB (or exported as a password-protected PKCS#12 file for backup). The private key **never** leaves the browser.

```
User's browser:
  1. Generate keypair (Web Crypto API)
  2. Store private key in IndexedDB
  3. Create Certificate Signing Request (CSR)
  4. Send CSR to admin (or CA)

Admin (Level 2):
  5. Sign CSR with company CA → produces signed certificate
  6. Return signed certificate to user

User's browser:
  7. Store signed certificate alongside private key
  8. Upload public certificate to wiki (for verification)
```

### 3.2 Signing flow

When confirming a reviewflow role:

```
1. Wiki presents the document digest to the browser
   (SHA-256 of the canonical markdown content + page version + timestamp)

2. Browser signs the digest with the user's private key
   (Web Crypto API — the key never leaves the browser)

3. Browser sends the signature + certificate to the wiki
   POST /api/plugin/reviewflow/v1/confirm/{path}
   {
     "role": "reviewer",
     "signature": "<base64-encoded signature>",
     "certificate": "<PEM-encoded certificate>",
     "digest": "<hex-encoded SHA-256>",
     "timestamp": "2026-03-19T10:30:00Z"
   }

4. Wiki verifies:
   - Certificate is valid (not expired, chains to a trusted CA)
   - Certificate belongs to the authenticated user (subject matches)
   - Signature is valid (digest matches, signed by this certificate's key)
   - Digest matches the current page content + version

5. Wiki stores the confirmation with the signature and certificate fingerprint
```

### 3.3 Verification

Anyone can verify a signature:

```
GET /api/plugin/reviewflow/v1/signatures/{path}?version=N

Response:
{
  "signatures": [
    {
      "role": "reviewer",
      "user": "bob",
      "timestamp": "2026-03-19T10:30:00Z",
      "digest": "a1b2c3...",
      "signature": "<base64>",
      "certificate_pem": "<PEM>",
      "certificate_fingerprint": "SHA256:ab:cd:ef:...",
      "verified": true
    }
  ],
  "document_digest": "a1b2c3...",
  "page_version": 42
}
```

The verification can be performed:
- By the wiki (automatic, on every status check)
- By an auditor (download the signature + certificate + document, verify independently with OpenSSL)
- By an automated compliance tool (via the API)

## 4. Data Model Extensions

### 4.1 User certificate (new)

Stored in user profile or as a separate file:

| Field | Type | Notes |
| --- | --- | --- |
| `username` | string | Owner |
| `certificate_pem` | string | PEM-encoded X.509 certificate |
| `fingerprint` | string | SHA-256 fingerprint |
| `issuer` | string | CA that signed the certificate |
| `not_before` | time | Certificate validity start |
| `not_after` | time | Certificate validity end |
| `uploaded_at` | time | When the certificate was registered |

### 4.2 Confirmation extension

The existing `Confirmation` struct gains optional fields:

| Field | Type | Notes |
| --- | --- | --- |
| `signature` | string | Base64-encoded cryptographic signature (null for click-only confirmations) |
| `digest` | string | SHA-256 of the signed content |
| `cert_fingerprint` | string | Certificate fingerprint used for signing |

These fields are nullable — existing click-only confirmations remain valid. The system supports mixed mode: some roles sign, others just click.

## 5. Company CA Setup (Level 2)

### 5.1 One-time setup (admin)

```bash
# Generate company root CA (once, keep private key secure)
openssl genrsa -aes256 -out company-ca.key 4096
openssl req -new -x509 -days 3650 -key company-ca.key \
  -out company-ca.crt \
  -subj "/O=GMT Science/CN=GMT Science Document Signing CA"

# Upload company-ca.crt to wiki admin as the trusted root
```

### 5.2 Per-user certificate issuance

```bash
# User generates CSR (or the browser does it via Web Crypto)
# Admin signs it:
openssl x509 -req -in alice.csr -CA company-ca.crt -CAkey company-ca.key \
  -CAcreateserial -out alice.crt -days 365 \
  -extfile <(echo "subjectAltName=email:alice@gmt.bio")
```

### 5.3 Wiki configuration

```yaml
reviewflow:
  signing:
    enabled: false              # Master switch
    required: false             # If true, click-only confirmations are rejected
    trust_store:                # List of trusted CA certificates
      - /path/to/company-ca.crt
    # Or use system trust store for external CAs:
    # trust_system: true
```

## 6. Frontend Flow

### 6.1 Key setup (one-time per browser)

1. User navigates to profile or "Signing Keys" section
2. Browser generates keypair via Web Crypto API
3. User downloads CSR (or admin scans a QR code)
4. Admin signs and returns the certificate
5. User imports the signed certificate
6. Browser stores: private key (IndexedDB, non-extractable) + certificate

### 6.2 Signing a confirmation

1. User clicks "Confirm" on a reviewflow panel
2. If the user has a signing key:
   - Browser computes the document digest
   - Browser signs the digest with the private key (Web Crypto API)
   - Sends signature + certificate to the API
3. If the user has no signing key:
   - Falls back to click-only confirmation (if `signing.required` is false)
   - Shows a warning: "Your confirmation is not cryptographically signed"

### 6.3 Visual indicators

| State | Display |
| --- | --- |
| Signed confirmation | Green lock icon next to the checkmark |
| Click-only confirmation | Simple checkmark (as today) |
| Signature verification failed | Red warning icon |
| Certificate expired | Orange warning icon |

## 7. Export: Validation Certificate

A dedicated view/PDF export showing all signatures for a validated version:

- Document title, path, version tag, content hash
- For each role: signer name, role, timestamp, certificate issuer, fingerprint, signature status
- The document's canonical content (or its digest)
- QR code linking to the online verification endpoint

This serves as the "validation certificate" that can be printed, archived, or presented to auditors.

## 8. Scope and Phases

### Phase 1 — Backend infrastructure
- Certificate storage model
- Signature verification logic (Go `crypto/x509`)
- Extended confirmation API (accept optional signature)
- Trust store configuration
- Signature query API

### Phase 2 — Browser key management
- Web Crypto API key generation
- CSR generation
- IndexedDB key storage
- Certificate import UI

### Phase 3 — Signing flow
- Digest computation (canonical markdown → SHA-256)
- Browser-side signing via Web Crypto
- Signed confirmation UI (lock icons, warnings)
- Mixed mode: signed + click-only coexistence

### Phase 4 — Admin CA tools
- Company CA generation wizard in admin UI
- CSR signing workflow
- Certificate revocation
- Trust store management

### Phase 5 — Audit and export
- Validation certificate view
- PDF export of validation certificates
- Independent verification endpoint
- Audit log of all signing events

## 9. Design Decisions

1. **Key backup** — deferred. For v1, if the user loses their browser data, they lose their private key and must generate a new one. PKCS#12 export may be added later.

2. **Certificate revocation** — simple revocation list stored in the wiki configuration. No CRL/OCSP infrastructure needed. The admin adds revoked certificate fingerprints to a list; the wiki rejects signatures from revoked certificates.

3. **Signed content** — the raw markdown of the page is signed, nothing else. The digest is `SHA-256(raw_markdown_bytes)`. The bijective dialect guarantees deterministic serialization, so the same content always produces the same digest. Metadata (version number, timestamps) is NOT part of the signed content — it's recorded alongside the signature but not covered by it.

4. **Trusted timestamps** — yes. Each signature will include an RFC 3161 timestamp from a free TSA (e.g., FreeTSA.org). This proves the signature was created at a specific time and was not backdated. The TSA response is stored alongside the signature.

5. **Hardware tokens** — deferred. WebAuthn/FIDO2 support may be added in a future phase for environments requiring hardware-bound keys.
