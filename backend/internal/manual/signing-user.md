# Document Signing — Users

This page explains how to use cryptographic signing when confirming your role in a reviewflow.

## 1. What signing does

When you sign a confirmation, your browser computes a cryptographic fingerprint of the document's exact content and signs it with your personal key. This proves:

- **You** confirmed (the signature is tied to your identity)
- **This exact content** was what you saw (any change would invalidate the signature)
- **At this time** (an independent timestamp is recorded)

Signed confirmations appear with a lock icon in the reviewflow panel, instead of a simple checkmark.

## 1. Generating your signing key

Before you can sign, you need a personal signing key.

1. Click your username in the top bar
2. Select **Signing Key**
3. Click **Generate Signing Key**

Your browser generates an ECDSA P-256 key pair. The private key never leaves your browser — it is stored in your browser's secure storage (IndexedDB) and cannot be exported or copied.

{blockquote class=warning}
> Your signing key is tied to this browser on this device. If you clear your browser data, switch browsers, or use a different computer, you will need to generate a new key.

## 1. Getting your key signed by the company CA

After generating your key, your public key is automatically sent to the server. An administrator can then sign it with the company CA (see [Signing Administration](./signing-admin)).

Until your key is CA-signed, your signatures are self-signed. They still prove you signed the document, but auditors cannot verify the link to your organization's trust chain.

## 1. Signing a confirmation

When signing is enabled on a page with a reviewflow, you will see a **Sign & Confirm** button instead of the regular Confirm button (provided you have a signing key).

1. Review the document content carefully
2. Click **Sign & Confirm** on your role

Your browser:
1. Computes a SHA-256 hash of the document's raw markdown
2. Signs that hash with your private key
3. Sends the signature, hash, and your certificate to the server
4. The server records the confirmation with the cryptographic evidence
5. An RFC 3161 timestamp is requested from an external time authority

The entire process takes less than a second.

## 1. What if signing is required?

If the administrator has made signing mandatory (`signing.required: true`), you must have a signing key to confirm. The confirm button will show "Signing key required" if you haven't generated one yet.

## 1. Key management

| Action | How |
| --- | --- |
| Check if you have a key | User menu > Signing Key |
| Generate a new key | User menu > Signing Key > Generate |
| View your certificate fingerprint | Shown in the Signing Key dialog after generation |

{blockquote class=note}
> If you lose your signing key (browser data cleared, new device), generate a new one. Your previous signatures remain valid — they are recorded on the server with your old certificate. An administrator will need to sign your new key with the company CA.
