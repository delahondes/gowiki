#!/usr/bin/env python3
"""
Gowiki Signature Verification Tool v2

Standalone tool for auditors to verify X.509 document signatures
exported from Gowiki's reviewflow system.

Usage:
    python3 verify.py audit-export.json [--ca ca-cert.pem]

The tool verifies:
1. Each signer certificate chains to the provided CA
2. Each certificate was valid at the time of signing (fail-closed on bad timestamps)
3. Each certificate has digitalSignature key usage
4. Each ECDSA/RSA signature matches the SHA-256 of the document markdown
5. The document markdown hash matches the declared digest
6. Signer certificate is not revoked at the time of signing (temporal revocation check)
7. RFC 3161 timestamp tokens: imprint binding AND TSA signature verified
   (requires asn1crypto; install it: pip install asn1crypto)

Signed payload definition:
  - The signer computes SHA-256 over the raw UTF-8 bytes of the markdown content
  - The ECDSA signature is computed over this 32-byte hash (Web Crypto API hashes internally)
  - The signature format is IEEE P1363 (r || s) for ECDSA P-256 (64 bytes)

No dependency on Gowiki — this tool uses only Python stdlib + cryptography.

Install: pip install cryptography
"""

import json
import sys
import base64
import hashlib
import argparse
from datetime import datetime, timezone

try:
    from cryptography import x509
    from cryptography.hazmat.primitives import hashes, serialization
    from cryptography.hazmat.primitives.asymmetric import ec, rsa, padding, utils
    from cryptography.x509 import load_pem_x509_certificate
    from cryptography.x509.oid import ExtensionOID
except ImportError:
    print("ERROR: 'cryptography' package required. Install: pip install cryptography")
    sys.exit(1)

# Optional: asn1crypto for RFC 3161 timestamp token validation.
try:
    from asn1crypto import tsp, cms
    HAS_ASN1CRYPTO = True
except ImportError:
    HAS_ASN1CRYPTO = False


def load_ca_cert(path):
    """Load a PEM CA certificate."""
    with open(path, "rb") as f:
        return load_pem_x509_certificate(f.read())


def verify_cert_chain(cert, ca_cert):
    """Verify that cert was signed by ca_cert. Returns (ok, message)."""
    try:
        ca_pub = ca_cert.public_key()
        if isinstance(ca_pub, ec.EllipticCurvePublicKey):
            ca_pub.verify(
                cert.signature,
                cert.tbs_certificate_bytes,
                ec.ECDSA(cert.signature_hash_algorithm),
            )
        elif isinstance(ca_pub, rsa.RSAPublicKey):
            ca_pub.verify(
                cert.signature,
                cert.tbs_certificate_bytes,
                padding.PKCS1v15(),
                cert.signature_hash_algorithm,
            )
        else:
            return False, f"unsupported CA key type: {type(ca_pub)}"
        return True, "chain verified"
    except Exception as e:
        return False, f"chain verification FAILED: {e}"


def check_key_usage(cert):
    """Check that the certificate has digitalSignature key usage."""
    try:
        ku = cert.extensions.get_extension_for_oid(ExtensionOID.KEY_USAGE)
        if not ku.value.digital_signature:
            return False, "certificate does NOT have digitalSignature key usage"
        return True, "digitalSignature present"
    except x509.ExtensionNotFound:
        # No KeyUsage extension — per X.509, all usages are implicitly allowed
        return True, "no KeyUsage extension (all usages permitted)"


def verify_confirmation(conf, ca_cert=None, doc_markdown=None, revoked_certs=None):
    """Verify a single signed confirmation. Returns (ok, messages)."""
    messages = []

    if not conf.get("signed"):
        return True, ["click-only confirmation (no cryptographic signature)"]

    cert_pem = conf.get("certificate_pem", "")
    sig_b64 = conf.get("signature_base64", "")
    digest_hex = conf.get("digest", "")
    timestamp_str = conf.get("timestamp", "")

    # --- Fail closed: all required fields must be present ---
    missing = []
    if not cert_pem:
        missing.append("certificate_pem")
    if not sig_b64:
        missing.append("signature_base64")
    if not digest_hex:
        missing.append("digest")
    if not timestamp_str:
        missing.append("timestamp")
    if missing:
        return False, [f"MISSING required fields: {', '.join(missing)}"]

    # --- Parse timestamp (fail closed) ---
    try:
        sign_time = datetime.fromisoformat(timestamp_str.replace("Z", "+00:00"))
    except Exception as e:
        return False, [f"INVALID timestamp format '{timestamp_str}': {e}"]

    # --- Parse the certificate ---
    try:
        cert = load_pem_x509_certificate(cert_pem.encode())
    except Exception as e:
        return False, [f"INVALID certificate: {e}"]

    messages.append(f"subject: {cert.subject}")
    messages.append(f"issuer: {cert.issuer}")

    # --- Certificate validity at signing time ---
    if sign_time < cert.not_valid_before_utc:
        return False, messages + [f"FAIL: cert not yet valid at signing time (valid from {cert.not_valid_before_utc})"]
    if sign_time > cert.not_valid_after_utc:
        return False, messages + [f"FAIL: cert expired at signing time (expired {cert.not_valid_after_utc})"]
    messages.append(f"cert valid at signing time ({cert.not_valid_before_utc} to {cert.not_valid_after_utc})")

    # --- Key usage check ---
    ku_ok, ku_msg = check_key_usage(cert)
    if not ku_ok:
        return False, messages + [f"FAIL: {ku_msg}"]
    messages.append(f"key usage: {ku_msg}")

    # --- CA chain verification ---
    if ca_cert is not None:
        chain_ok, chain_msg = verify_cert_chain(cert, ca_cert)
        if not chain_ok:
            return False, messages + [f"FAIL: {chain_msg}"]
        messages.append(f"CA chain: {chain_msg}")
    else:
        messages.append("CA chain: NOT VERIFIED (no CA provided)")

    # --- Fingerprint consistency ---
    fp_stored = conf.get("certificate_fingerprint", "")
    fp_computed = hashlib.sha256(cert.public_bytes(serialization.Encoding.DER)).hexdigest()
    if fp_stored and fp_stored != fp_computed:
        return False, messages + [f"FAIL: fingerprint mismatch (stored={fp_stored[:16]}..., computed={fp_computed[:16]}...)"]
    messages.append(f"fingerprint: {fp_computed[:32]}...")

    # --- Revocation check (temporal: only revocations before signing time invalidate) ---
    if revoked_certs:
        revoked_entry = None
        for rc in revoked_certs:
            # Support both new format (dict with fingerprint+revoked_at) and legacy (plain string).
            if isinstance(rc, dict):
                if rc.get("fingerprint") == fp_computed:
                    revoked_entry = rc
                    break
            elif isinstance(rc, str) and rc == fp_computed:
                revoked_entry = {"fingerprint": rc, "revoked_at": ""}
                break
        if revoked_entry:
            revoked_at_str = revoked_entry.get("revoked_at", "")
            if revoked_at_str:
                try:
                    revoked_at = datetime.fromisoformat(revoked_at_str.replace("Z", "+00:00"))
                    if sign_time >= revoked_at:
                        return False, messages + [
                            f"FAIL: certificate REVOKED on {revoked_at_str} — signature at {timestamp_str} is AFTER revocation"
                        ]
                    else:
                        messages.append(f"revocation: cert revoked on {revoked_at_str} but signature predates revocation — VALID")
                except Exception:
                    # Cannot parse revocation date — fail closed.
                    return False, messages + [f"FAIL: certificate is REVOKED (cannot parse revocation date '{revoked_at_str}')"]
            else:
                # No revocation date — fail closed (legacy format, no temporal info).
                return False, messages + [f"FAIL: certificate is REVOKED (no revocation date — cannot determine validity)"]
        else:
            messages.append("revocation: not revoked")
    else:
        messages.append("revocation: no revocation list provided")

    # --- Digest verification against document content ---
    if doc_markdown is not None:
        computed_digest = hashlib.sha256(doc_markdown.encode("utf-8")).hexdigest()
        if digest_hex != computed_digest:
            return False, messages + [f"FAIL: digest mismatch (confirmation={digest_hex[:16]}..., markdown={computed_digest[:16]}...)"]
        messages.append("digest matches document markdown")
    else:
        messages.append("digest: cannot verify (no markdown in export)")

    # --- Signature verification ---
    sig_bytes = base64.b64decode(sig_b64)
    # The signed payload is SHA-256(raw_utf8_markdown_bytes).
    # The browser computes SHA-256 internally via Web Crypto ECDSA with hash:SHA-256.
    # We verify against the same hash.
    digest_bytes = bytes.fromhex(digest_hex)

    pub_key = cert.public_key()
    try:
        if isinstance(pub_key, ec.EllipticCurvePublicKey):
            # Web Crypto produces P1363 (r||s), convert to DER.
            key_size = pub_key.key_size
            byte_size = (key_size + 7) // 8
            if len(sig_bytes) == 2 * byte_size:
                r = int.from_bytes(sig_bytes[:byte_size], "big")
                s = int.from_bytes(sig_bytes[byte_size:], "big")
                der_sig = utils.encode_dss_signature(r, s)
            else:
                der_sig = sig_bytes
            pub_key.verify(der_sig, digest_bytes, ec.ECDSA(utils.Prehashed(hashes.SHA256())))
        elif isinstance(pub_key, rsa.RSAPublicKey):
            pub_key.verify(sig_bytes, digest_bytes, padding.PKCS1v15(), utils.Prehashed(hashes.SHA256()))
        else:
            return False, messages + [f"FAIL: unsupported key type {type(pub_key)}"]
    except Exception as e:
        return False, messages + [f"FAIL: SIGNATURE INVALID — {e}"]

    messages.append("SIGNATURE VALID")

    # --- Timestamp token ---
    ts_token = conf.get("timestamp_token", "")
    if ts_token:
        try:
            ts_bytes = base64.b64decode(ts_token)
            messages.append(f"TSA timestamp token: present ({len(ts_bytes)} bytes)")

            if HAS_ASN1CRYPTO:
                try:
                    ts_resp = tsp.TimeStampResp.load(ts_bytes)
                    status_val = ts_resp["status"]["status"].native
                    if status_val != "granted" and status_val != "granted_with_mods":
                        messages.append(f"TSA timestamp: REJECTED by TSA (status={status_val})")
                    else:
                        ts_token_obj = ts_resp["time_stamp_token"]
                        signed_data = ts_token_obj["content"]
                        tst_info = signed_data["encap_content_info"]["content"].parsed
                        ts_time = tst_info["gen_time"].native
                        imprint = tst_info["message_imprint"]
                        imprint_hash = imprint["hashed_message"].native
                        imprint_algo = imprint["hash_algorithm"]["algorithm"].native

                        # 1. Verify message imprint matches SHA-256 of our signature.
                        sig_hash = hashlib.sha256(base64.b64decode(sig_b64)).digest()
                        if imprint_hash == sig_hash:
                            messages.append(f"TSA imprint: VERIFIED — matches signature hash")
                        else:
                            messages.append(f"TSA imprint: MISMATCH — token does not match this signature")

                        messages.append(f"TSA timestamp time: {ts_time}")
                        messages.append(f"TSA hash algorithm: {imprint_algo}")

                        # 2. Verify TSA signature on the token.
                        signer_infos = signed_data["signer_infos"]
                        if len(signer_infos) > 0:
                            signer_info = signer_infos[0]
                            tsa_sig = signer_info["signature"].native
                            signed_attrs = signer_info["signed_attrs"]

                            # Extract TSA certificate from the token.
                            tsa_certs = signed_data["certificates"]
                            tsa_verified = False
                            if tsa_certs and len(tsa_certs) > 0:
                                tsa_cert_der = tsa_certs[0].chosen.dump()
                                tsa_cert = load_pem_x509_certificate(
                                    b"-----BEGIN CERTIFICATE-----\n" +
                                    base64.encodebytes(tsa_cert_der) +
                                    b"-----END CERTIFICATE-----\n"
                                )
                                messages.append(f"TSA certificate: {tsa_cert.subject}")

                                # Verify the TSA signature over the signed attributes.
                                try:
                                    # The signed content is the DER-encoded signed_attrs
                                    # with EXPLICIT SET tag (0x31) instead of IMPLICIT (0xA0).
                                    signed_attrs_der = signed_attrs.dump()
                                    # Replace the implicit tag with SET tag for verification.
                                    signed_attrs_der = b"\x31" + signed_attrs_der[1:]

                                    tsa_pub = tsa_cert.public_key()
                                    sig_algo = signer_info["signature_algorithm"]
                                    hash_algo_name = sig_algo.hash_algo
                                    if hash_algo_name == "sha256":
                                        h = hashes.SHA256()
                                    elif hash_algo_name == "sha384":
                                        h = hashes.SHA384()
                                    elif hash_algo_name == "sha512":
                                        h = hashes.SHA512()
                                    else:
                                        h = hashes.SHA256()

                                    if isinstance(tsa_pub, rsa.RSAPublicKey):
                                        tsa_pub.verify(tsa_sig, signed_attrs_der, padding.PKCS1v15(), h)
                                        tsa_verified = True
                                    elif isinstance(tsa_pub, ec.EllipticCurvePublicKey):
                                        tsa_pub.verify(tsa_sig, signed_attrs_der, ec.ECDSA(h))
                                        tsa_verified = True
                                except Exception as e:
                                    messages.append(f"TSA signature: VERIFICATION FAILED — {e}")

                            if tsa_verified:
                                messages.append("TSA signature: VERIFIED — token was signed by the TSA")
                            elif not tsa_certs or len(tsa_certs) == 0:
                                messages.append("TSA signature: NO TSA CERTIFICATE in token (cannot verify)")
                        else:
                            messages.append("TSA signature: no signer info in token")
                except Exception as e:
                    messages.append(f"TSA timestamp: parse error — {e}")
            else:
                messages.append("TSA timestamp: NOT VALIDATED (install asn1crypto for full verification: pip install asn1crypto)")
        except Exception:
            messages.append("TSA timestamp token: INVALID base64")
    else:
        messages.append("TSA timestamp token: absent")

    return True, messages


def main():
    parser = argparse.ArgumentParser(
        description="Gowiki Signature Verification Tool — verifies X.509 document signatures",
        epilog="Signed payload: SHA-256 of the raw UTF-8 markdown bytes, signed with ECDSA P-256 (P1363 format)."
    )
    parser.add_argument("audit_file", help="Path to the audit export JSON file")
    parser.add_argument("--ca", help="Path to the CA certificate PEM file (overrides embedded CA)")
    parser.add_argument("--verbose", "-v", action="store_true", help="Show detailed per-confirmation output")
    args = parser.parse_args()

    with open(args.audit_file) as f:
        audit = json.load(f)

    # --- Load CA certificate ---
    ca_cert = None
    if args.ca:
        ca_cert = load_ca_cert(args.ca)
        print(f"CA: {ca_cert.subject} (from file: {args.ca})")
    elif audit.get("ca_certificate_pem"):
        ca_cert = load_pem_x509_certificate(audit["ca_certificate_pem"].encode())
        print(f"CA: {ca_cert.subject} (embedded in export)")
    else:
        print("CA: NONE — certificate chain will NOT be verified")
    print()

    # --- Document info ---
    print(f"Page: {audit['page']}")
    if audit.get("page_url"):
        print(f"URL: {audit['page_url']}")
    print(f"Version: {audit['version']} (tag: {audit.get('version_tag', '?')})")
    print(f"Document SHA-256: {audit.get('markdown_sha256', 'N/A')}")
    print(f"Exported at: {audit.get('exported_at', '?')}")

    # --- Verify markdown integrity ---
    doc_markdown = audit.get("markdown")
    if doc_markdown:
        computed = hashlib.sha256(doc_markdown.encode("utf-8")).hexdigest()
        declared = audit.get("markdown_sha256", "")
        if computed == declared:
            print(f"Markdown integrity: VERIFIED (SHA-256 matches)")
        else:
            print(f"Markdown integrity: MISMATCH! computed={computed[:16]}... declared={declared[:16]}...")
            print("WARNING: The markdown content does not match the declared hash!")
    else:
        print("Markdown: not included in export (cannot verify content integrity)")
    print()

    # --- Verify confirmations ---
    confirmations = audit.get("confirmations", [])
    if not confirmations:
        print("No confirmations found.")
        sys.exit(0)

    print(f"Confirmations: {len(confirmations)}")
    print("-" * 60)

    all_ok = True
    for conf in confirmations:
        role = conf.get("role", "?")
        user = conf.get("user", "?")
        ts = conf.get("timestamp", "?")

        revoked_list = audit.get("revoked_certs") or []
        ok, messages = verify_confirmation(conf, ca_cert, doc_markdown, revoked_list)
        status = "PASS" if ok else "FAIL"
        icon = "\u2713" if ok else "\u2717"

        print(f"\n  {icon} {role} ({user}) @ {ts}: {status}")
        if args.verbose or not ok:
            for msg in messages:
                print(f"    {msg}")

        if not ok:
            all_ok = False

    print()
    print("=" * 60)
    if all_ok:
        print("RESULT: ALL SIGNATURES VERIFIED \u2713")
    else:
        print("RESULT: SOME SIGNATURES FAILED \u2717")

    # --- Known limitations ---
    print()
    print("Known limitations:")
    if HAS_ASN1CRYPTO:
        print("  - RFC 3161: imprint and TSA signature verified; TSA certificate chain NOT verified against a TSA trust anchor")
    else:
        print("  - RFC 3161 timestamp tokens: NOT validated (install asn1crypto: pip install asn1crypto)")
    print("  - Certificate purpose (EKU) is not strictly enforced beyond KeyUsage.digitalSignature")
    print("  - Chain validation is single-level (leaf -> CA), not a full PKI engine")
    print("  - TSA certificate chain is not verified against an external trust anchor")

    sys.exit(0 if all_ok else 1)


if __name__ == "__main__":
    main()
