// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

package oauth

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStateIsRandom(t *testing.T) {
	a, err := State()
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	b, _ := State()
	if a == "" || a == b {
		t.Fatalf("State should return distinct non-empty tokens: %q %q", a, b)
	}
}

func TestRegistryRegistersOnlyConfigured(t *testing.T) {
	r := NewRegistry("gid", "gsecret", "", "") // github creds missing
	if _, ok := r.Get("google"); !ok {
		t.Error("google should be registered")
	}
	if _, ok := r.Get("github"); ok {
		t.Error("github must not be registered without credentials")
	}
	if names := r.Names(); len(names) != 1 || names[0] != "google" {
		t.Errorf("Names() = %v, want [google]", names)
	}
}

func TestAuthCodeURL(t *testing.T) {
	p, _ := NewRegistry("gid", "gsecret", "", "").Get("google")
	verifier, _ := PKCEVerifier()
	u := p.AuthCodeURL("state-xyz", PKCEChallenge(verifier), "https://app.example/auth/google/callback")
	for _, want := range []string{
		"client_id=gid", "state=state-xyz", "response_type=code",
		"redirect_uri=https", "code_challenge=", "code_challenge_method=S256",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("AuthCodeURL missing %q: %s", want, u)
		}
	}
}

func TestPKCEChallengeDeterministic(t *testing.T) {
	v, _ := PKCEVerifier()
	if PKCEChallenge(v) != PKCEChallenge(v) {
		t.Fatal("PKCEChallenge must be deterministic for a given verifier")
	}
	v2, _ := PKCEVerifier()
	if v == v2 {
		t.Fatal("PKCEVerifier must yield distinct values per call")
	}
}

func TestIdentityGoogle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_, _ = io.WriteString(w, `{"access_token":"tok123"}`)
		case "/userinfo":
			if r.Header.Get("Authorization") != "Bearer tok123" {
				t.Errorf("userinfo missing bearer token, got %q", r.Header.Get("Authorization"))
			}
			_, _ = io.WriteString(w, `{"email":"alice@example.com","email_verified":true}`)
		}
	}))
	defer srv.Close()

	p := &provider{
		name: "google", tokenURL: srv.URL + "/token", emailURL: srv.URL + "/userinfo",
		http: srv.Client(), parseEmail: parseGoogleEmail,
	}
	id, err := p.Identity(context.Background(), "code1", "verifier1", "https://app/cb")
	if err != nil {
		t.Fatalf("Identity: %v", err)
	}
	if id.Email != "alice@example.com" || !id.Verified {
		t.Fatalf("Identity = %+v", id)
	}
}

func TestIdentityGitHubPicksPrimaryEmail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_, _ = io.WriteString(w, `{"access_token":"tok"}`)
		case "/emails":
			_, _ = io.WriteString(w, `[
				{"email":"alt@example.com","primary":false,"verified":true},
				{"email":"primary@example.com","primary":true,"verified":true}
			]`)
		}
	}))
	defer srv.Close()

	p := &provider{
		name: "github", tokenURL: srv.URL + "/token", emailURL: srv.URL + "/emails",
		http: srv.Client(), parseEmail: parseGitHubEmail,
	}
	id, err := p.Identity(context.Background(), "c", "v", "https://app/cb")
	if err != nil {
		t.Fatalf("Identity: %v", err)
	}
	if id.Email != "primary@example.com" {
		t.Fatalf("should pick the primary email, got %+v", id)
	}
}
