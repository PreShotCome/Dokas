package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/preshotcome/dokaz/internal/audit"
	"github.com/preshotcome/dokaz/internal/auth"
	"github.com/preshotcome/dokaz/internal/mobileauth"
)

// mobileTokenCtxKey carries the authenticated mobile token (for logout/revoke).
// Reuses the v1CtxKey type with a distinct value so it never collides with the
// API-key context value.
const mobileTokenCtxKey v1CtxKey = 1

func mobileTokenFromContext(ctx context.Context) (mobileauth.Token, bool) {
	t, ok := ctx.Value(mobileTokenCtxKey).(mobileauth.Token)
	return t, ok
}

// dummyMobileHash is verified against when an email doesn't resolve, so the
// login path takes the same time whether or not the account exists.
const dummyMobileHash = dummyHash

type mobileLoginReq struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	DeviceName string `json:"device_name"`
}

type mobileUser struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

// mobileLogin authenticates email+password and either issues a bearer token or,
// when the account has MFA enabled, returns a 202 with a challenge the app
// completes at /mobile/mfa-verify. Mirrors loginSubmit's credential checks
// (Argon2id verify, per-email throttle) but speaks JSON and never sets cookies.
func (h *Handlers) mobileLogin(w http.ResponseWriter, r *http.Request) {
	var req mobileLoginReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	email := strings.TrimSpace(strings.ToLower(req.Email))
	if email == "" || req.Password == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "email and password are required")
		return
	}

	if lock, err := h.throttle.Check(r.Context(), email); err == nil && lock.Locked {
		w.Header().Set("Retry-After", strconv.Itoa(int(lock.RetryAfter.Seconds())+1))
		writeAPIError(w, http.StatusTooManyRequests, "too_many_requests",
			"too many failed attempts; try again later")
		return
	}

	clientIP := audit.ClientIP(r)

	var id uuid.UUID
	var hash, dbEmail string
	var mfaEnabled bool
	err := h.pool.QueryRow(r.Context(), `
		SELECT id, email, password_hash, mfa_enabled FROM users
		 WHERE email = $1 AND deleted_at IS NULL
	`, email).Scan(&id, &dbEmail, &hash, &mfaEnabled)
	if err != nil {
		_ = auth.VerifyPassword(req.Password, dummyMobileHash) // constant-ish time
		_ = h.throttle.Record(r.Context(), email, clientIP, false)
		writeAPIError(w, http.StatusUnauthorized, "unauthorized", "invalid email or password")
		return
	}
	if err := auth.VerifyPassword(req.Password, hash); err != nil {
		_ = h.throttle.Record(r.Context(), email, clientIP, false)
		_ = h.audit.Record(r.Context(), audit.Event{
			Action: "mobile.login.failed", TargetID: id.String(),
			IP: clientIP, UserAgent: r.UserAgent(),
		})
		writeAPIError(w, http.StatusUnauthorized, "unauthorized", "invalid email or password")
		return
	}
	_ = h.throttle.Record(r.Context(), email, clientIP, true)

	acctID := h.pickDefaultAccount(r.Context(), id)
	if acctID == uuid.Nil {
		writeAPIError(w, http.StatusForbidden, "no_account", "user has no workspace")
		return
	}

	// MFA gate: hand back a challenge; no token until the code is verified.
	if mfaEnabled {
		ch, err := h.mobileTokens.CreateChallenge(r.Context(), id, acctID, req.DeviceName)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "internal", "could not start MFA challenge")
			return
		}
		writeData(w, http.StatusAccepted, map[string]any{
			"mfa_required": true,
			"challenge_id": ch.String(),
		}, nil)
		return
	}

	h.issueMobileToken(w, r, id, acctID, dbEmail, req.DeviceName)
}

type mobileMFAVerifyReq struct {
	ChallengeID string `json:"challenge_id"`
	Code        string `json:"code"`
}

// mobileMFAVerify completes the MFA step: it consumes the challenge, verifies a
// TOTP code (or a single-use recovery code), and issues a token.
func (h *Handlers) mobileMFAVerify(w http.ResponseWriter, r *http.Request) {
	var req mobileMFAVerifyReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	chID, err := uuid.Parse(strings.TrimSpace(req.ChallengeID))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "challenge_id must be a valid UUID")
		return
	}
	code := strings.TrimSpace(req.Code)
	if code == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "code is required")
		return
	}

	ch, err := h.mobileTokens.ConsumeChallenge(r.Context(), chID)
	if err != nil {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized", "challenge expired or not found")
		return
	}

	ok, err := h.sessions.VerifyAndConsumeTOTP(r.Context(), ch.UserID, code)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", "could not verify code")
		return
	}
	if !ok {
		// Fall back to a recovery code before rejecting.
		used, rerr := h.sessions.ConsumeRecoveryCode(r.Context(), ch.UserID, code)
		if rerr != nil {
			writeAPIError(w, http.StatusInternalServerError, "internal", "could not verify code")
			return
		}
		if !used {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "invalid code")
			return
		}
	}

	var email string
	_ = h.pool.QueryRow(r.Context(), `SELECT email FROM users WHERE id = $1`, ch.UserID).Scan(&email)
	h.issueMobileToken(w, r, ch.UserID, ch.AccountID, email, ch.DeviceName)
}

// issueMobileToken mints a token and writes the success envelope.
func (h *Handlers) issueMobileToken(w http.ResponseWriter, r *http.Request, userID, accountID uuid.UUID, email, deviceName string) {
	tok, err := h.mobileTokens.Create(r.Context(), userID, accountID, deviceName, mobileauth.DefaultTTL)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", "could not issue token")
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: &accountID, ActorID: &userID, Action: "mobile.login.succeeded",
		IP: audit.ClientIP(r), UserAgent: r.UserAgent(),
	})
	writeData(w, http.StatusOK, map[string]any{
		"token":      tok.Secret,
		"token_type": "Bearer",
		"expires_at": tok.ExpiresAt,
		"account_id": accountID.String(),
		"user":       mobileUser{ID: userID.String(), Email: email},
	}, nil)
}

// mobileLogout revokes the calling token.
func (h *Handlers) mobileLogout(w http.ResponseWriter, r *http.Request) {
	tok, ok := mobileTokenFromContext(r.Context())
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
		return
	}
	if err := h.mobileTokens.Revoke(r.Context(), tok.UserID, tok.ID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", "could not revoke token")
		return
	}
	writeData(w, http.StatusOK, map[string]any{"revoked": true}, nil)
}

type mobileDeviceReq struct {
	Token    string `json:"token"`
	Platform string `json:"platform"`
}

// mobileRegisterDevice upserts the caller's push (FCM) token so heartbeat/drill
// alerts can reach this device. Idempotent — the app calls it on every launch.
func (h *Handlers) mobileRegisterDevice(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.FromContext(r.Context())
	acct, _ := auth.CurrentAccountFromContext(r.Context())
	var req mobileDeviceReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if strings.TrimSpace(req.Token) == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "token is required")
		return
	}
	id, err := h.pushDevices.Register(r.Context(), user.ID, acct.ID, req.Token, req.Platform)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", "could not register device")
		return
	}
	writeData(w, http.StatusOK, map[string]any{"id": id.String()}, nil)
}

// mobileDeleteDevice removes one of the caller's registered devices.
func (h *Handlers) mobileDeleteDevice(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.FromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "device not found")
		return
	}
	if err := h.pushDevices.Delete(r.Context(), user.ID, id); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", "could not delete device")
		return
	}
	writeData(w, http.StatusOK, map[string]any{"deleted": true}, nil)
}

// mobileBearerAuth resolves the Bearer mobile token to a (user, account),
// verifies the user is still a member of that account, and stamps the account,
// user, and token onto the request context — the same shape the reused /v1
// read handlers expect.
func (h *Handlers) mobileBearerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := bearerToken(r)
		if !ok {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized",
				"missing or malformed Authorization: Bearer header")
			return
		}
		tok, err := h.mobileTokens.Authenticate(r.Context(), raw)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "invalid or expired token")
			return
		}
		// Membership may have been revoked since the token was issued.
		if _, err := h.accounts.GetMembership(r.Context(), tok.AccountID, tok.UserID); err != nil {
			writeAPIError(w, http.StatusForbidden, "forbidden", "no access to this workspace")
			return
		}
		acct, err := h.accounts.GetAccount(r.Context(), tok.AccountID)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "account unavailable")
			return
		}
		user, err := h.sessions.LoadUserByID(r.Context(), tok.UserID)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "user unavailable")
			return
		}
		ctx := auth.WithCurrentAccount(r.Context(), &acct)
		ctx = auth.WithUser(ctx, user)
		ctx = context.WithValue(ctx, mobileTokenCtxKey, tok)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// mobileRouter is the whole /mobile subtree: unauthenticated login endpoints
// (per-IP rate limited) plus a token-authenticated read API. The read routes
// reuse the /v1 handlers (which read the account from context) — no API-key
// scopes, since the responder app is read-only and gated by membership.
func (h *Handlers) mobileRouter() http.Handler {
	r := chi.NewRouter()

	// CORS: the responder app may run as a web build (Flutter web / local dev
	// in a browser), which makes cross-origin calls. The /mobile API is
	// Bearer-token only (no cookies), so reflecting the origin is safe — there
	// are no ambient credentials for a malicious page to ride on. Preflight
	// (OPTIONS) is answered here before auth/rate-limit middleware.
	r.Use(mobileCORS)

	// Unauthenticated: log in / complete MFA. Per-IP limited like the web
	// login routes, to blunt credential-stuffing.
	r.Group(func(r chi.Router) {
		r.Use(h.authLimiter.Middleware(clientIPKey))
		r.Post("/login", h.mobileLogin)
		r.Post("/mfa-verify", h.mobileMFAVerify)
	})

	// Token-authenticated subtree.
	r.Group(func(r chi.Router) {
		r.Use(h.mobileBearerAuth)
		r.Use(h.v1Limiter.Middleware(v1AccountKey))

		r.Post("/logout", h.mobileLogout)

		// Push device registry.
		r.Post("/devices", h.mobileRegisterDevice)
		r.Delete("/devices/{id}", h.mobileDeleteDevice)

		// Read-only resources (reused /v1 handlers).
		r.Get("/databases", h.v1ListDatabases)
		r.Get("/databases/{id}", h.v1GetDatabase)
		r.Get("/drills", h.v1ListDrills)
		r.Get("/drills/{id}", h.v1GetDrill)
		r.Get("/drills/{id}/evidence", h.v1GetEvidence)
		r.Get("/drills/{id}/signature", h.v1GetSignature)
		r.Get("/drills/{id}/logs", h.v1GetLogs)
		r.Get("/heartbeats", h.v1ListHeartbeats)
		r.Get("/heartbeats/{id}", h.v1GetHeartbeat)
		r.Get("/alerts", h.v1ListAlerts)
	})

	return r
}

// mobileCORS allows browser (web build) clients to call the token-authenticated
// mobile API. Origin is reflected (not credentialed — Bearer tokens, no
// cookies), and OPTIONS preflight is short-circuited with 204.
func mobileCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key")
		w.Header().Set("Access-Control-Max-Age", "600")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
