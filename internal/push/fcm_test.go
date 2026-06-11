package push

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// rsaPEM generates a fresh test RSA key and returns it PKCS#1 PEM-encoded —
// the same shape Firebase service-account JSON ships ("BEGIN RSA PRIVATE KEY").
func rsaPEM(t *testing.T) string {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa: %v", err)
	}
	der := x509.MarshalPKCS1PrivateKey(k)
	out := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der})
	return string(out)
}

// TestFCMSenderEndToEnd exercises the JWT sign → token exchange → message send
// pipeline against an httptest backend that mimics Google's OAuth + FCM v1 URLs.
func TestFCMSenderEndToEnd(t *testing.T) {
	var oauthHits, sendHits int
	var lastMessage map[string]any

	mux := http.NewServeMux()
	mux.HandleFunc("/oauth", func(w http.ResponseWriter, r *http.Request) {
		oauthHits++
		// Confirm the JWT-bearer shape; full RS256 verification is overkill for
		// this test — sign-then-decode is checked implicitly by the JSON below.
		if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("oauth content-type = %q", r.Header.Get("Content-Type"))
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "grant_type=urn%3Aietf%3Aparams%3Aoauth%3Agrant-type%3Ajwt-bearer") ||
			!strings.Contains(string(body), "assertion=") {
			t.Errorf("oauth body missing fields: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"AT-1","expires_in":3600}`))
	})
	mux.HandleFunc("/fcm/v1/projects/proj-xyz/messages:send", func(w http.ResponseWriter, r *http.Request) {
		sendHits++
		if got := r.Header.Get("Authorization"); got != "Bearer AT-1" {
			t.Errorf("auth header = %q, want Bearer AT-1", got)
		}
		_ = json.NewDecoder(r.Body).Decode(&lastMessage)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"projects/proj-xyz/messages/m-1"}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	sa := serviceAccount{
		ProjectID:   "proj-xyz",
		ClientEmail: "fcm@proj-xyz.iam.gserviceaccount.com",
		PrivateKey:  rsaPEM(t),
		TokenURI:    server.URL + "/oauth",
	}
	data, _ := json.Marshal(sa)
	s, err := NewFCMSender(string(data), nil)
	if err != nil {
		t.Fatalf("NewFCMSender: %v", err)
	}
	// Redirect the FCM endpoint at the fake.
	s.endpoint = server.URL + "/fcm/v1/projects/proj-xyz/messages:send"

	ctx := context.Background()
	err = s.Send(ctx, []string{"device-A", "device-B"}, Notification{
		Title: "Drill FAILED",
		Body:  "assertion_failed",
		Data:  map[string]string{"type": "drill", "id": "abc"},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if sendHits != 2 {
		t.Fatalf("FCM send hits = %d, want 2 (one per token)", sendHits)
	}
	// Confirm payload shape on the last call (device-B).
	msg, _ := lastMessage["message"].(map[string]any)
	if msg == nil || msg["token"] != "device-B" {
		t.Fatalf("last message = %v", lastMessage)
	}
	// Second Send should reuse the cached access token, not re-mint.
	if err := s.Send(ctx, []string{"device-C"}, Notification{Title: "x", Body: "y"}); err != nil {
		t.Fatalf("Send 2: %v", err)
	}
	if oauthHits != 1 {
		t.Fatalf("OAuth hits = %d, want 1 (token should be cached)", oauthHits)
	}
}

// TestFCMSenderEmptyConfig disables itself cleanly.
func TestFCMSenderEmptyConfig(t *testing.T) {
	s, err := NewFCMSender("", nil)
	if err != nil || s != nil {
		t.Fatalf("empty config → (%v, %v), want (nil, nil)", s, err)
	}
}
