// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

package push

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// FCMSender implements Sender against the Firebase Cloud Messaging HTTP v1 API.
// Auth: a Google service-account JWT signed locally (no SDK) exchanged for an
// OAuth2 access token; the token is cached and refreshed transparently.
type FCMSender struct {
	projectID string
	email     string
	key       *rsa.PrivateKey
	tokenURI  string
	endpoint  string
	http      *http.Client
	logger    *slog.Logger

	mu       sync.Mutex
	token    string
	tokenExp time.Time
}

// serviceAccount is the subset of a Google service-account JSON we use.
type serviceAccount struct {
	ProjectID   string `json:"project_id"`
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

// NewFCMSender reads either a JSON string or a path to a JSON file. Returns
// nil, nil when src is empty (the caller falls back to LogSender).
func NewFCMSender(src string, logger *slog.Logger) (*FCMSender, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil, nil
	}
	var data []byte
	if strings.HasPrefix(src, "{") {
		data = []byte(src)
	} else {
		b, err := os.ReadFile(src)
		if err != nil {
			return nil, fmt.Errorf("read service account file: %w", err)
		}
		data = b
	}
	var sa serviceAccount
	if err := json.Unmarshal(data, &sa); err != nil {
		return nil, fmt.Errorf("parse service account: %w", err)
	}
	if sa.ProjectID == "" || sa.ClientEmail == "" || sa.PrivateKey == "" {
		return nil, errors.New("service account missing project_id, client_email, or private_key")
	}
	key, err := parsePrivateKey(sa.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	tokenURI := sa.TokenURI
	if tokenURI == "" {
		tokenURI = "https://oauth2.googleapis.com/token"
	}
	return &FCMSender{
		projectID: sa.ProjectID,
		email:     sa.ClientEmail,
		key:       key,
		tokenURI:  tokenURI,
		endpoint:  fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", sa.ProjectID),
		http:      &http.Client{Timeout: 15 * time.Second},
		logger:    logger,
	}, nil
}

// Send delivers n to every token. FCM HTTP v1 is one-message-per-call; we loop
// per token and never let one failure block the rest — a single bad token only
// loses its own delivery.
func (s *FCMSender) Send(ctx context.Context, tokens []string, n Notification) error {
	if len(tokens) == 0 {
		return nil
	}
	access, err := s.accessToken(ctx)
	if err != nil {
		return err
	}
	var firstErr error
	for _, t := range tokens {
		if err := s.sendOne(ctx, access, t, n); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			if s.logger != nil {
				s.logger.Warn("fcm send failed", "token_prefix", short(t), "err", err)
			}
		}
	}
	return firstErr
}

func (s *FCMSender) sendOne(ctx context.Context, access, token string, n Notification) error {
	msg := map[string]any{
		"message": map[string]any{
			"token":        token,
			"notification": map[string]string{"title": n.Title, "body": n.Body},
			"data":         n.Data,
		},
	}
	body, _ := json.Marshal(msg)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("fcm: %d %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// accessToken returns a cached OAuth2 access token, minting a new one when the
// cached one is missing or about to expire.
func (s *FCMSender) accessToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && time.Until(s.tokenExp) > 60*time.Second {
		return s.token, nil
	}
	jwt, err := s.signJWT()
	if err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", jwt)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return "", fmt.Errorf("oauth token: %d %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	s.token = out.AccessToken
	s.tokenExp = time.Now().Add(time.Duration(out.ExpiresIn) * time.Second)
	return s.token, nil
}

// signJWT builds the RS256-signed assertion that swaps for an access token.
func (s *FCMSender) signJWT() (string, error) {
	now := time.Now()
	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	claims := map[string]any{
		"iss":   s.email,
		"scope": "https://www.googleapis.com/auth/firebase.messaging",
		"aud":   s.tokenURI,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	}
	hb, _ := json.Marshal(header)
	cb, _ := json.Marshal(claims)
	enc := base64.RawURLEncoding
	signing := enc.EncodeToString(hb) + "." + enc.EncodeToString(cb)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signing + "." + enc.EncodeToString(sig), nil
}

func parsePrivateKey(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("not a PEM block")
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	k, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("not an RSA private key")
	}
	return k, nil
}

func short(s string) string {
	if len(s) <= 8 {
		return s
	}
	return s[:8] + "…"
}
