// Package oauth implements OAuth 2.0 "sign in with" social login for Google
// and GitHub. It talks to each provider's REST endpoints directly over
// net/http — no SDK — consistent with the rest of the codebase.
//
// A provider is only registered when both its client ID and secret are
// configured; the login page shows a button per registered provider.
package oauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Identity is what a provider tells us about the signing-in user.
type Identity struct {
	Email    string
	Verified bool
}

// Provider is one OAuth identity provider.
type Provider interface {
	// Name is the URL slug ("google", "github").
	Name() string
	// AuthCodeURL is where the user is sent to authorize, carrying a CSRF
	// state and the callback redirect URI.
	AuthCodeURL(state, redirectURI string) string
	// Identity exchanges an authorization code for the user's email.
	Identity(ctx context.Context, code, redirectURI string) (Identity, error)
}

// State returns a random URL-safe CSRF state token for the OAuth flow.
func State() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// provider is the shared OAuth-code-flow implementation; the per-provider
// difference is the endpoints, the scope, and how the email is parsed.
type provider struct {
	name         string
	clientID     string
	clientSecret string
	authURL      string
	tokenURL     string
	emailURL     string
	scope        string
	http         *http.Client
	parseEmail   func([]byte) (Identity, error)
}

func (p *provider) Name() string { return p.name }

func (p *provider) AuthCodeURL(state, redirectURI string) string {
	q := url.Values{
		"client_id":     {p.clientID},
		"redirect_uri":  {redirectURI},
		"response_type": {"code"},
		"scope":         {p.scope},
		"state":         {state},
	}
	return p.authURL + "?" + q.Encode()
}

func (p *provider) Identity(ctx context.Context, code, redirectURI string) (Identity, error) {
	token, err := p.exchange(ctx, code, redirectURI)
	if err != nil {
		return Identity{}, err
	}
	body, err := p.get(ctx, p.emailURL, token)
	if err != nil {
		return Identity{}, err
	}
	return p.parseEmail(body)
}

// exchange swaps an authorization code for an access token.
func (p *provider) exchange(ctx context.Context, code, redirectURI string) (string, error) {
	form := url.Values{
		"client_id":     {p.clientID},
		"client_secret": {p.clientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.tokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json") // GitHub returns form-encoded without this
	body, err := p.do(req)
	if err != nil {
		return "", err
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", fmt.Errorf("oauth %s: decode token: %w", p.name, err)
	}
	if tok.AccessToken == "" {
		return "", fmt.Errorf("oauth %s: token response had no access_token", p.name)
	}
	return tok.AccessToken, nil
}

// get fetches a bearer-authenticated JSON resource.
func (p *provider) get(ctx context.Context, rawURL, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "soteria") // GitHub rejects requests without one
	return p.do(req)
}

func (p *provider) do(req *http.Request) ([]byte, error) {
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oauth %s: %w", p.name, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("oauth %s: %s", p.name, resp.Status)
	}
	return body, nil
}

// --- Google ---

func parseGoogleEmail(body []byte) (Identity, error) {
	var u struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := json.Unmarshal(body, &u); err != nil {
		return Identity{}, fmt.Errorf("oauth google: decode userinfo: %w", err)
	}
	if u.Email == "" {
		return Identity{}, errors.New("oauth google: userinfo had no email")
	}
	return Identity{Email: u.Email, Verified: u.EmailVerified}, nil
}

// --- GitHub ---

func parseGitHubEmail(body []byte) (Identity, error) {
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := json.Unmarshal(body, &emails); err != nil {
		return Identity{}, fmt.Errorf("oauth github: decode emails: %w", err)
	}
	for _, e := range emails {
		if e.Primary {
			return Identity{Email: e.Email, Verified: e.Verified}, nil
		}
	}
	return Identity{}, errors.New("oauth github: account has no primary email")
}

// --- registry ---

// Registry holds the configured providers.
type Registry struct {
	providers map[string]Provider
}

// NewRegistry builds the provider registry from configuration. A provider is
// registered only when both its client ID and secret are set.
func NewRegistry(googleID, googleSecret, githubID, githubSecret string) *Registry {
	r := &Registry{providers: map[string]Provider{}}
	httpClient := &http.Client{Timeout: 10 * time.Second}
	if googleID != "" && googleSecret != "" {
		r.providers["google"] = &provider{
			name: "google", clientID: googleID, clientSecret: googleSecret,
			authURL:    "https://accounts.google.com/o/oauth2/v2/auth",
			tokenURL:   "https://oauth2.googleapis.com/token",
			emailURL:   "https://openidconnect.googleapis.com/v1/userinfo",
			scope:      "openid email",
			http:       httpClient,
			parseEmail: parseGoogleEmail,
		}
	}
	if githubID != "" && githubSecret != "" {
		r.providers["github"] = &provider{
			name: "github", clientID: githubID, clientSecret: githubSecret,
			authURL:    "https://github.com/login/oauth/authorize",
			tokenURL:   "https://github.com/login/oauth/access_token",
			emailURL:   "https://api.github.com/user/emails",
			scope:      "read:user user:email",
			http:       httpClient,
			parseEmail: parseGitHubEmail,
		}
	}
	return r
}

// Get returns a registered provider by name.
func (r *Registry) Get(name string) (Provider, bool) {
	p, ok := r.providers[name]
	return p, ok
}

// Names lists the registered provider slugs, sorted — used to render the
// login buttons.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.providers))
	for n := range r.providers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
