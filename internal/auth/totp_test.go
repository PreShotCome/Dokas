package auth

import (
	"encoding/base32"
	"strings"
	"testing"
	"time"
)

// rfcSecret is the RFC 6238 test secret ("12345678901234567890"), base32-encoded.
func rfcSecret() string {
	return base32.StdEncoding.WithPadding(base32.NoPadding).
		EncodeToString([]byte("12345678901234567890"))
}

func TestTOTPCodeRFC6238(t *testing.T) {
	secret := rfcSecret()
	// RFC 6238 SHA-1 vectors, truncated to the 6 digits we use.
	cases := []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1234567890, "005924"},
	}
	for _, c := range cases {
		got, err := TOTPCode(secret, time.Unix(c.unix, 0))
		if err != nil {
			t.Fatalf("TOTPCode(%d): %v", c.unix, err)
		}
		if got != c.want {
			t.Errorf("TOTPCode at %d = %s, want %s", c.unix, got, c.want)
		}
	}
}

func TestVerifyTOTPSkew(t *testing.T) {
	secret := rfcSecret()
	base := time.Unix(59, 0)
	if !VerifyTOTP(secret, "287082", base) {
		t.Error("code should verify at its own step")
	}
	if !VerifyTOTP(secret, "287082", base.Add(30*time.Second)) {
		t.Error("code should verify one step late (clock skew tolerance)")
	}
	if VerifyTOTP(secret, "287082", base.Add(120*time.Second)) {
		t.Error("code should not verify four steps later")
	}
	if VerifyTOTP(secret, "000000", base) {
		t.Error("a wrong code must not verify")
	}
	if VerifyTOTP(secret, "12345", base) {
		t.Error("a malformed (short) code must not verify")
	}
}

func TestTOTPRoundTrip(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	now := time.Now()
	code, err := TOTPCode(secret, now)
	if err != nil {
		t.Fatalf("TOTPCode: %v", err)
	}
	if !VerifyTOTP(secret, code, now) {
		t.Fatal("a freshly generated code should verify")
	}
}

func TestTOTPURI(t *testing.T) {
	uri := TOTPURI("ABCDEF234567", "user@example.com")
	if !strings.HasPrefix(uri, "otpauth://totp/") {
		t.Errorf("unexpected URI prefix: %s", uri)
	}
	for _, want := range []string{"secret=ABCDEF234567", "issuer=Soteria", "digits=6", "period=30"} {
		if !strings.Contains(uri, want) {
			t.Errorf("URI missing %q: %s", want, uri)
		}
	}
}

func TestGenerateRecoveryCodesUnique(t *testing.T) {
	codes, err := GenerateRecoveryCodes()
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}
	if len(codes) != recoveryCodeCount {
		t.Fatalf("got %d codes, want %d", len(codes), recoveryCodeCount)
	}
	seen := make(map[string]bool)
	for _, c := range codes {
		if seen[c] {
			t.Fatalf("duplicate recovery code %q", c)
		}
		seen[c] = true
	}
}
