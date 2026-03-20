# Document Signing — Trust Model & Verification

This page documents the security model, trust assumptions, and verification procedures for Gowiki's document signing system. It is intended for auditors, quality managers, and anyone evaluating the integrity guarantees of signed documents.

## 1. Security model summary

Gowiki's signing system provides cryptographic proof that a specific person confirmed a specific document at a specific time. The guarantees are:

| Property | Mechanism |
| --- | --- |
| **Document integrity** | SHA-256 hash of the raw UTF-8 markdown bytes |
| **Signer identity** | ECDSA P-256 signature with X.509 certificate |
| **Certificate trust** | Certificate signed by organization's CA |
| **Timestamp** | RFC 3161 timestamp token from external TSA |
| **Tamper evidence** | Any modification to the document invalidates all signatures |

## 1. What is signed

The signed payload is precisely defined:

> **SHA-256 of the raw UTF-8 bytes of the markdown document**, signed with **ECDSA P-256** in **IEEE P1363 format** (64 bytes: r || s, 32 bytes each).

The signer's browser computes SHA-256 over the exact markdown content, then signs the resulting 32-byte hash using the Web Crypto API. The signature is computed by the browser — the private key never reaches the server.

## 1. Trust anchors

For the verification to be meaningful, three trust anchors must be accepted:

### The Company CA

The organization generates a root Certificate Authority (ECDSA P-256) hosted on the Gowiki server. All user certificates are signed by this CA.

**Trust assumption:** the CA private key is protected and only used by authorized administrators. Anyone with access to the CA key could issue certificates for arbitrary users.

**Mitigation:** the CA key is stored in `data/meta/_ca/` on the server, accessible only to the server process and system administrators.

### The Timestamp Authority (TSA)

When a confirmation is recorded, the server requests an RFC 3161 timestamp from an external TSA. The default is FreeTSA.org, a free public timestamp authority.

**Trust assumption:** the TSA is honest and its clock is accurate. The TSA signs a timestamp token that binds the exact signature to a point in time.

**What the TSA sees:** only a SHA-256 hash of the signature, not the document content. The TSA cannot read the document.

**What the TSA proves:** the signature existed at the claimed time. It does not prove the document existed before that time — only that the signature over the hash was presented to the TSA at that moment.

{blockquote class=note}
> **Trust model note:** By default, the verifier trusts any TSA certificate embedded in the timestamp token without anchoring it to a predefined root store. This is equivalent to trusting the TSA service configured at signing time. For higher assurance environments, the verifier should be configured with a pinned TSA root certificate or a restricted trust store.

### The Gowiki server

The server stores confirmations, assembles the audit export, and mediates the signing flow. The server cannot forge signatures (it never has access to user private keys), but it controls:
- Which markdown content is presented for signing
- Which confirmations are recorded
- The timing of TSA requests

**Trust assumption:** the server operates correctly and is not compromised.

**Residual risk:** If the server is compromised, it could present altered content to the user for signing while displaying different content in the UI. This risk is inherent to any server-mediated signing flow. The audit export and offline verification ensure that the signed content can be independently validated after the fact.

**Mitigation:** the audit export is self-contained and can be verified offline using only the standalone verifier and the CA certificate.

## 1. PKI model

The PKI is intentionally simplified for a closed organizational system:

| Feature | Status |
| --- | --- |
| Certificate chain depth | 1 level (leaf -> CA) |
| Path building | Direct: leaf must chain to the single configured CA |
| Key usage enforcement | `digitalSignature` required on leaf certificates |
| Extended Key Usage (EKU) | Not enforced |
| Certificate validity period | Checked against signing time |
| Revocation | Fingerprint-based list in configuration, no CRL/OCSP |
| Policy constraints | Not implemented |

This model is appropriate for an organization where:
- The CA is managed by a small number of trusted administrators
- All signers are employees or known collaborators
- The number of certificates is manageable (tens to hundreds, not thousands)

This PKI model is designed for a **closed organizational trust domain** and is not intended to interoperate with public PKI ecosystems (e.g., WebPKI). For environments requiring full PKI (CRL, OCSP, path constraints, qualified certificates), the trust store and revocation list can be extended, but the core verification logic remains single-level.

## 1. Revocation model

Revocation is managed through a list of SHA-256 certificate fingerprints in the server configuration:

```yaml
signing:
  revoked_certs:
    - "a1b2c3d4..."
```

**Scope:** this is an administratively maintained list. There is no automated CRL distribution or OCSP responder. The list is:
- Checked by the server when a signature is submitted (revoked certs are rejected)
- Included in every audit export (so the verifier can check it)
- Under the control of the server administrator

**Limitation:** revocation is only as current as the last configuration update. There is no real-time revocation check.

**Important:** Revocation is evaluated at verification time, not at signing time. A certificate that was valid when the signature was created but later revoked will be flagged as invalid during verification if it appears in the revocation list.

## 1. Audit export

The audit export is a self-contained JSON file that includes everything needed for offline verification:

| Field | Description |
| --- | --- |
| `markdown` | The full document content that was signed |
| `markdown_sha256` | SHA-256 hex digest of the markdown |
| `ca_certificate_pem` | The organization's CA certificate (PEM) |
| `signed_payload_spec` | Exact definition of what is signed |
| `revoked_certs` | List of revoked certificate fingerprints |
| `confirmations` | Array of per-role confirmation records |
| `exported_at` | Timestamp of the export |

Each confirmation record includes:
- Role name and confirming user
- Signature (base64, ECDSA P1363)
- Signer certificate (PEM)
- Digest (SHA-256 hex)
- RFC 3161 timestamp token (base64 DER, when available)

Download the audit export from the reviewflow panel on any validated page, or via the API:

```
GET /api/plugin/reviewflow/v1/audit/{page-path}?version={N}
```

## 1. Standalone verification

A Python verification tool is provided in `tools/verify-signatures/verify.py`. It is independent of Gowiki and uses only standard cryptography libraries.

```bash
pip install cryptography asn1crypto
python3 verify.py audit-export.json
```

The verifier checks:
1. Document hash matches the declared `markdown_sha256`
2. Each signer certificate chains to the CA in the export
3. Each certificate was valid at the time of signing
4. Each certificate has `digitalSignature` key usage
5. No certificate is on the revocation list
6. Each ECDSA signature is valid for the document hash
7. Each RFC 3161 timestamp imprint matches the signature
8. Each RFC 3161 timestamp token signature is valid (TSA signature verification)

Optionally supply an external CA certificate to override the one in the export:

```bash
python3 verify.py audit-export.json --ca company-ca.pem
```

## 1. Known limitations

These are design choices, not bugs. They are documented here for transparency.

| Limitation | Explanation |
| --- | --- |
| **No CRL/OCSP** | Revocation is a static fingerprint list, not a live protocol. Acceptable for small organizations; requires manual updates. |
| **Single-level PKI** | No intermediate CAs, no path constraints. Sufficient for a closed system with one organizational CA. |
| **TSA trust is implicit** | The TSA certificate in the timestamp token is trusted without anchoring to an external root set. For qualified timestamps, use a qualified TSA. |
| **Server mediates the flow** | The server presents content and records confirmations. A compromised server could in theory present different content for signing. The offline verifier mitigates this for post-hoc auditing. |
| **Browser key storage** | Private keys are stored in IndexedDB (Web Crypto, non-extractable). Keys are lost if browser data is cleared. This is a usability trade-off — no server-side key escrow. |
| **No multi-device keys** | A signing key is tied to one browser on one device. Users on multiple devices need multiple keys (and multiple CA-signed certificates). |

## 1. Formal statement

The signature verification mechanism demonstrates strong internal consistency and correctly implements cryptographic verification of document integrity, signature validity, and timestamp binding. The exported evidence is self-contained and reproducible. The trust anchors (Company CA, TSA, and server) are clearly scoped, and the remaining simplifications (single-level PKI, static revocation, implicit TSA trust) are appropriate for a closed organizational deployment and do not invalidate the core signature verification.
