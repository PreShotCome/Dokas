package webhooks

import (
	"strings"
	"testing"
)

func TestSignDeterministic(t *testing.T) {
	body := []byte(`{"event":"drill.completed"}`)
	a := Sign("whsec_abc", body)
	b := Sign("whsec_abc", body)
	if a != b {
		t.Fatalf("Sign not deterministic: %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, "sha256=") {
		t.Fatalf("signature missing sha256= prefix: %q", a)
	}
}

func TestSignDiffersBySecret(t *testing.T) {
	body := []byte("payload")
	if Sign("secret-one", body) == Sign("secret-two", body) {
		t.Fatal("different secrets must produce different signatures")
	}
}

func TestSignDiffersByBody(t *testing.T) {
	if Sign("s", []byte("a")) == Sign("s", []byte("b")) {
		t.Fatal("different bodies must produce different signatures")
	}
}

func TestVerify(t *testing.T) {
	body := []byte(`{"ok":true}`)
	sig := Sign("whsec_xyz", body)

	if !Verify("whsec_xyz", body, sig) {
		t.Fatal("Verify should accept a signature it produced")
	}
	if Verify("whsec_xyz", []byte("tampered"), sig) {
		t.Fatal("Verify should reject a tampered body")
	}
	if Verify("wrong-secret", body, sig) {
		t.Fatal("Verify should reject a wrong secret")
	}
	if Verify("whsec_xyz", body, "sha256=deadbeef") {
		t.Fatal("Verify should reject a garbage signature")
	}
}
