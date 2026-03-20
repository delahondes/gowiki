package reviewflow

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultTSAURL = "https://freetsa.org/tsr"

// RequestTimestamp sends a TimeStampReq to a TSA and returns the base64-encoded
// TimeStampResp token. The token proves the digest existed at the time of the response.
//
// We use a simplified approach: send the SHA-256 digest directly as a raw hash
// to the TSA. Most TSAs accept this via a simple POST with content-type
// application/timestamp-query.
//
// For full RFC 3161 compliance, we'd need to build an ASN.1 TimeStampReq.
// For now, we use FreeTSA's simplified HTTP API which accepts a hash directly.
func RequestTimestamp(digest []byte, tsaURL string) (string, error) {
	if tsaURL == "" {
		tsaURL = defaultTSAURL
	}

	// Build a minimal RFC 3161 TimeStampReq.
	// FreeTSA accepts a POST with the DER-encoded TimeStampReq.
	tsReq, err := buildTimestampRequest(digest)
	if err != nil {
		return "", fmt.Errorf("build timestamp request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(tsaURL, "application/timestamp-query", bytes.NewReader(tsReq))
	if err != nil {
		return "", fmt.Errorf("TSA request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("TSA returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read TSA response: %w", err)
	}

	return base64.StdEncoding.EncodeToString(body), nil
}

// buildTimestampRequest creates a minimal ASN.1 DER-encoded RFC 3161 TimeStampReq.
// Structure: SEQUENCE { version INTEGER(1), messageImprint SEQUENCE { algorithm, hash }, nonce, certReq }
func buildTimestampRequest(digest []byte) ([]byte, error) {
	// SHA-256 OID: 2.16.840.1.101.3.4.2.1
	sha256OID := []byte{0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x01}

	// AlgorithmIdentifier: SEQUENCE { OID, NULL }
	algID := asn1Sequence(append(asn1OID(sha256OID), asn1Null()...))

	// MessageImprint: SEQUENCE { AlgorithmIdentifier, OCTET STRING hash }
	msgImprint := asn1Sequence(append(algID, asn1OctetString(digest)...))

	// Version: INTEGER 1
	version := asn1Integer(1)

	// CertReq: BOOLEAN TRUE (request the TSA to include its cert)
	certReq := asn1Boolean(true)

	// TimeStampReq: SEQUENCE { version, messageImprint, certReq }
	tsReq := asn1Sequence(append(append(version, msgImprint...), certReq...))

	return tsReq, nil
}

// ComputeTimestampDigest returns the SHA-256 hash of the signature bytes
// (the TSA timestamps the signature, not the document).
func ComputeTimestampDigest(signatureB64 string) ([]byte, error) {
	sigBytes, err := base64.StdEncoding.DecodeString(signatureB64)
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(sigBytes)
	return h[:], nil
}

// --- Minimal ASN.1 DER encoding helpers ---

func asn1Sequence(content []byte) []byte {
	return asn1TLV(0x30, content)
}

func asn1OID(oid []byte) []byte {
	return asn1TLV(0x06, oid)
}

func asn1OctetString(data []byte) []byte {
	return asn1TLV(0x04, data)
}

func asn1Integer(val int) []byte {
	if val < 128 {
		return asn1TLV(0x02, []byte{byte(val)})
	}
	// Multi-byte integer (not needed for version=1)
	return asn1TLV(0x02, []byte{byte(val)})
}

func asn1Null() []byte {
	return []byte{0x05, 0x00}
}

func asn1Boolean(val bool) []byte {
	if val {
		return asn1TLV(0x01, []byte{0xFF})
	}
	return asn1TLV(0x01, []byte{0x00})
}

func asn1TLV(tag byte, value []byte) []byte {
	l := len(value)
	if l < 128 {
		return append([]byte{tag, byte(l)}, value...)
	}
	// Long form length
	if l < 256 {
		return append([]byte{tag, 0x81, byte(l)}, value...)
	}
	return append([]byte{tag, 0x82, byte(l >> 8), byte(l)}, value...)
}
