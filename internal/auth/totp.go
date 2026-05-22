package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP parameters — the defaults every authenticator app expects (RFC 6238).
const (
	totpPeriod = 30 * time.Second
	totpDigits = 6
	totpIssuer = "Soteria"
)

// totpEncoding is the standard uppercase base32 alphabet with no padding —
// the form authenticator apps accept for the shared secret.
var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateTOTPSecret returns a fresh base32-encoded 20-byte TOTP secret.
func GenerateTOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return totpEncoding.EncodeToString(b), nil
}

// TOTPCode computes the RFC 6238 code for a base32 secret at time t.
func TOTPCode(secret string, t time.Time) (string, error) {
	key, err := totpEncoding.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", fmt.Errorf("auth: invalid TOTP secret: %w", err)
	}
	counter := uint64(t.UTC().Unix()) / uint64(totpPeriod.Seconds())

	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)

	// Dynamic truncation (RFC 4226 §5.3).
	offset := sum[len(sum)-1] & 0x0f
	bin := (uint32(sum[offset]&0x7f) << 24) |
		(uint32(sum[offset+1]) << 16) |
		(uint32(sum[offset+2]) << 8) |
		uint32(sum[offset+3])

	mod := uint32(1)
	for i := 0; i < totpDigits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", totpDigits, bin%mod), nil
}

// VerifyTOTP reports whether code is valid for secret at time t, allowing a
// ±1 step (±30s) skew for clock drift between the server and the device.
func VerifyTOTP(secret, code string, t time.Time) bool {
	_, ok := VerifyTOTPWithCounter(secret, code, t)
	return ok
}

// VerifyTOTPWithCounter is VerifyTOTP but also returns the time-step counter
// the code matched. Callers persist that counter to reject replays: a code is
// valid across a ±1-step window (~90s), so without replay tracking an
// observed code could be re-used until it expires.
func VerifyTOTPWithCounter(secret, code string, t time.Time) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return 0, false
	}
	for _, skew := range []time.Duration{0, -totpPeriod, totpPeriod} {
		at := t.Add(skew)
		want, err := TOTPCode(secret, at)
		if err != nil {
			return 0, false
		}
		if hmac.Equal([]byte(want), []byte(code)) {
			return int64(uint64(at.UTC().Unix()) / uint64(totpPeriod.Seconds())), true
		}
	}
	return 0, false
}

// TOTPURI builds the otpauth:// URI an authenticator app imports — usually as
// a QR code, but it can also be typed in.
func TOTPURI(secret, account string) string {
	label := url.PathEscape(totpIssuer + ":" + account)
	q := url.Values{
		"secret":    {secret},
		"issuer":    {totpIssuer},
		"algorithm": {"SHA1"},
		"digits":    {fmt.Sprint(totpDigits)},
		"period":    {fmt.Sprint(int(totpPeriod.Seconds()))},
	}
	return "otpauth://totp/" + label + "?" + q.Encode()
}
