package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/preshotcome/anything/internal/apikey"
	"github.com/preshotcome/anything/internal/auth"
)

// --- response envelope ---

// apiError is one error in the {data,meta,errors} envelope.
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type envelope struct {
	Data   any        `json:"data,omitempty"`
	Meta   any        `json:"meta,omitempty"`
	Errors []apiError `json:"errors,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, env envelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(env)
}

func writeData(w http.ResponseWriter, status int, data, meta any) {
	writeJSON(w, status, envelope{Data: data, Meta: meta})
}

func writeAPIError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, envelope{Errors: []apiError{{Code: code, Message: msg}}})
}

// --- v1 context ---

type v1CtxKey int

const apiKeyCtxKey v1CtxKey = 0

func apiKeyFromContext(ctx context.Context) (apikey.Key, bool) {
	k, ok := ctx.Value(apiKeyCtxKey).(apikey.Key)
	return k, ok
}

// --- router ---

// v1Router builds the /v1 subtree: API-key auth, per-account rate limiting,
// then the resource endpoints.
func (h *Handlers) v1Router() http.Handler {
	r := chi.NewRouter()
	r.Use(h.v1Authenticate)
	r.Use(h.v1Limiter.Middleware(v1AccountKey))

	r.Get("/databases", h.v1ListDatabases)
	r.Get("/databases/{id}", h.v1GetDatabase)
	r.Get("/drills", h.v1ListDrills)
	r.Get("/drills/{id}", h.v1GetDrill)
	r.Get("/drills/{id}/evidence", h.v1GetEvidence)

	// State-changing endpoints require an Idempotency-Key.
	r.Group(func(r chi.Router) {
		r.Use(h.v1Idempotency)
		r.Post("/databases", h.v1CreateDatabase)
		r.Post("/drills", h.v1CreateDrill)
	})
	return r
}

// v1AccountKey buckets the rate limiter by the authenticated account.
func v1AccountKey(r *http.Request) string {
	if a, ok := auth.CurrentAccountFromContext(r.Context()); ok {
		return "v1:" + a.ID.String()
	}
	return "v1:anon"
}

// v1Authenticate resolves the Bearer API key to an account and stamps both
// the account and the key onto the request context.
func (h *Handlers) v1Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := bearerToken(r)
		if !ok {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized",
				"missing or malformed Authorization: Bearer header")
			return
		}
		key, err := h.apiKeys.Authenticate(r.Context(), raw)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "invalid API key")
			return
		}
		acct, err := h.accounts.GetAccount(r.Context(), key.AccountID)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "account unavailable")
			return
		}
		ctx := auth.WithCurrentAccount(r.Context(), &acct)
		ctx = context.WithValue(ctx, apiKeyCtxKey, key)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func bearerToken(r *http.Request) (string, bool) {
	const p = "Bearer "
	h := r.Header.Get("Authorization")
	if len(h) <= len(p) || h[:len(p)] != p {
		return "", false
	}
	return h[len(p):], true
}

// --- idempotency ---

// v1Idempotency enforces the Idempotency-Key header on writes. A new key is
// processed and its response stored; a repeated key replays the stored
// response; a key reused with a different request body is a 409.
func (h *Handlers) v1Idempotency(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		acct, _ := auth.CurrentAccountFromContext(r.Context())
		key := r.Header.Get("Idempotency-Key")
		if key == "" {
			writeAPIError(w, http.StatusBadRequest, "idempotency_key_required",
				"POST requests require an Idempotency-Key header")
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "could not read request body")
			return
		}
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(body))
		fingerprint := fingerprintRequest(r.Method, r.URL.Path, body)

		// Replay path: a stored response for this (account, key).
		var storedFP string
		var storedStatus int
		var storedBody []byte
		err = h.pool.QueryRow(r.Context(), `
			SELECT request_fingerprint, status_code, response_body
			  FROM api_idempotency WHERE account_id = $1 AND idempotency_key = $2
		`, acct.ID, key).Scan(&storedFP, &storedStatus, &storedBody)
		if err == nil {
			if storedFP != fingerprint {
				writeAPIError(w, http.StatusConflict, "idempotency_key_reused",
					"this Idempotency-Key was used with a different request")
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Idempotency-Replayed", "true")
			w.WriteHeader(storedStatus)
			_, _ = w.Write(storedBody)
			return
		}

		// Fresh: capture the handler's response, then persist it.
		rec := &captureWriter{ResponseWriter: w, status: http.StatusOK, buf: &bytes.Buffer{}}
		next.ServeHTTP(rec, r)

		_, _ = h.pool.Exec(r.Context(), `
			INSERT INTO api_idempotency
			    (account_id, idempotency_key, request_fingerprint, status_code, response_body)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (account_id, idempotency_key) DO NOTHING
		`, acct.ID, key, fingerprint, rec.status, rec.buf.Bytes())
	})
}

// captureWriter records the response so the idempotency layer can persist
// and later replay it, while still streaming to the client.
type captureWriter struct {
	http.ResponseWriter
	status int
	buf    *bytes.Buffer
}

func (c *captureWriter) WriteHeader(status int) {
	c.status = status
	c.ResponseWriter.WriteHeader(status)
}

func (c *captureWriter) Write(b []byte) (int, error) {
	c.buf.Write(b)
	return c.ResponseWriter.Write(b)
}

func fingerprintRequest(method, path string, body []byte) string {
	sum := sha256.New()
	sum.Write([]byte(method))
	sum.Write([]byte{0})
	sum.Write([]byte(path))
	sum.Write([]byte{0})
	sum.Write(body)
	return base64.RawStdEncoding.EncodeToString(sum.Sum(nil))
}

// --- cursor pagination ---

const (
	v1DefaultLimit = 25
	v1MaxLimit     = 100
)

// pageCursor is the opaque cursor for keyset pagination over rows ordered by
// (created_at DESC, id DESC).
type pageCursor struct {
	CreatedAt time.Time `json:"t"`
	ID        uuid.UUID `json:"i"`
}

func encodeCursor(c pageCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (pageCursor, error) {
	var c pageCursor
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return c, errors.New("malformed cursor")
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, errors.New("malformed cursor")
	}
	return c, nil
}

// parsePageParams reads ?limit= and ?cursor= from the request.
func parsePageParams(r *http.Request) (limit int, cursor *pageCursor, err error) {
	limit = v1DefaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, convErr := strconv.Atoi(v)
		if convErr != nil || n < 1 {
			return 0, nil, errors.New("limit must be a positive integer")
		}
		if n > v1MaxLimit {
			n = v1MaxLimit
		}
		limit = n
	}
	if v := r.URL.Query().Get("cursor"); v != "" {
		c, decErr := decodeCursor(v)
		if decErr != nil {
			return 0, nil, decErr
		}
		cursor = &c
	}
	return limit, cursor, nil
}

// listMeta is the meta block for a paginated list response.
type listMeta struct {
	Count      int    `json:"count"`
	NextCursor string `json:"next_cursor,omitempty"`
}
