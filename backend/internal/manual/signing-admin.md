# Document Signing — Administration

This page covers the setup and management of X.509 document signing for administrators.

## 1. Overview

Document signing adds cryptographic proof to reviewflow confirmations. When enabled, users sign the exact content of a document when they confirm their role. This creates a tamper-evident record: any change to the document after signing would invalidate the signature.

Signing is optional by default. It can be made mandatory per configuration.

## 1. Enabling signing

In Admin > Configuration > Reviewflow, set:

```
signing:
  enabled: true
  required: false
```

| Setting | Description |
| --- | --- |
| `enabled` | Show "Sign & Confirm" buttons to users who have a signing key |
| `required` | Require cryptographic signatures for all confirmations (users without keys cannot confirm) |

## 1. Company CA (Certificate Authority)

Gowiki includes a built-in Certificate Authority that signs user certificates. This creates a trust chain: each user's signing key is endorsed by the company CA, and auditors can verify that chain.

### Generating the CA

Go to Admin > Certificates and click **Generate Company CA**.

This creates an ECDSA P-256 root certificate stored on the server. The CA is generated once and used to sign all user certificates.

{blockquote class=warning}
> The CA private key is stored on the server in `data/meta/_ca/`. Protect this directory. If the CA key is compromised, all certificates signed by it should be considered untrusted.

### Downloading the CA certificate

After generation, use the **Download CA Certificate** button to obtain the public certificate (PEM format). This file is needed by auditors to verify signatures.

## 1. Managing user certificates

When a user generates a signing key (see [Signing for Users](./signing-user)), their public key is sent to the server. The admin can then sign it with the company CA.

### Signing a user's key

In Admin > Certificates, the pending certificates table shows users who have generated keys but whose certificates have not yet been signed by the CA. Click **Sign** to issue a CA-signed certificate.

Once signed, the user's confirmations will chain to the company CA, which auditors can independently verify.

### Viewing certificates

The certificates table shows all issued certificates with:
- Username
- Fingerprint (SHA-256)
- Validity dates (not before / not after)
- Whether the certificate is CA-signed or self-signed

## 1. Revoking certificates

If a user's key is compromised or they leave the organization, add their certificate fingerprint to the revocation list:

```yaml
signing:
  revoked_certs:
    - "a1b2c3d4e5f6..."
```

The fingerprint is the SHA-256 hash of the certificate, visible in the certificates table.

Revoked certificates:
- Are flagged in the audit export (`revoked_certs` field)
- Are checked by the standalone verification tool
- Cannot be used for new confirmations (the server rejects them)

## 1. Trust store

The `trust_store` configuration accepts paths to additional trusted CA PEM files. This is useful if you need to trust external CAs in addition to the built-in company CA:

```yaml
signing:
  trust_store:
    - /etc/gowiki/external-ca.pem
```

In most deployments, the built-in company CA is sufficient and `trust_store` can be left empty.

## 1. Configuration reference

```yaml
reviewflow:
  enabled: true
  signing:
    enabled: true         # Enable signing UI
    required: false       # Make signing mandatory
    trust_store: []       # Additional trusted CA certificates
    revoked_certs: []     # SHA-256 fingerprints of revoked certs
```
