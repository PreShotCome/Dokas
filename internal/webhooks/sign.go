package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// SignatureHeader is the HTTP header carrying the HMAC signature.
const SignatureHeader = "X-RestoreDrill-Signature"

// Sign returns the value for the SignatureHeader: "sha256=" followed by the
// hex-encoded HMAC-SHA256 of body keyed by secret.
//
// Receivers verify by recomputing this over the raw request body and
// comparing in constant time.
func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Verify reports whether sig is a valid signature for body under secret.
// Provided for tests and a future inbound-webhook receiver.
func Verify(secret string, body []byte, sig string) bool {
	expected := Sign(secret, body)
	return hmac.Equal([]byte(expected), []byte(sig))
}
