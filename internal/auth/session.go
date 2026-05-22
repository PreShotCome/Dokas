package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Cookie name uses the __Host- prefix in production (requires HTTPS + no Domain attr).
// In dev we fall back to a plain name so cookies work without TLS.
const (
	cookieNameSecure   = "__Host-so_session"
	cookieNameInsecure = "so_session"
)

var ErrNoSession = errors.New("no session")

type Session struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	CreatedAt time.Time
	ExpiresAt time.Time
	// ImpersonatorID, when set, is the staff user impersonating UserID.
	ImpersonatorID *uuid.UUID
	// MFAPending is true between a correct password and a correct MFA code:
	// the session exists but must not authenticate app requests.
	MFAPending bool
	// StaffVerifiedAt is the last time this session re-proved staff identity
	// via an SSO step-up. nil means it never has.
	StaffVerifiedAt *time.Time
}

type User struct {
	ID            uuid.UUID
	Email         string
	EmailVerified bool
	IsStaff       bool
	MFAEnabled    bool
	CreatedAt     time.Time
}

type Store struct {
	pool           *pgxpool.Pool
	idleTimeout    time.Duration
	absoluteMaxAge time.Duration
	secure         bool
}

func NewStore(pool *pgxpool.Pool, idle, max time.Duration, secure bool) *Store {
	return &Store{pool: pool, idleTimeout: idle, absoluteMaxAge: max, secure: secure}
}

func (s *Store) cookieName() string {
	if s.secure {
		return cookieNameSecure
	}
	return cookieNameInsecure
}

// Create issues a fully authenticated session for a user and writes the
// cookie. currentAccountID is optional (pass uuid.Nil for legacy callers);
// when non-zero it's persisted on the session row so subsequent requests
// resolve the same account context without a per-user lookup.
func (s *Store) Create(ctx context.Context, w http.ResponseWriter, userID, currentAccountID uuid.UUID) error {
	return s.create(ctx, w, userID, currentAccountID, false)
}

// CreatePending issues a session that has passed the password check but still
// owes an MFA code. It does not authenticate app requests until CompleteMFA
// promotes it.
func (s *Store) CreatePending(ctx context.Context, w http.ResponseWriter, userID uuid.UUID) error {
	return s.create(ctx, w, userID, uuid.Nil, true)
}

func (s *Store) create(ctx context.Context, w http.ResponseWriter, userID, currentAccountID uuid.UUID, mfaPending bool) error {
	raw, hash, err := generateToken()
	if err != nil {
		return err
	}
	id := uuid.New()
	now := time.Now().UTC()
	expires := now.Add(s.absoluteMaxAge)

	var accountArg any
	if currentAccountID != uuid.Nil {
		accountArg = currentAccountID
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO sessions
		    (id, user_id, token_hash, created_at, last_seen_at, expires_at, current_account_id, mfa_pending)
		VALUES ($1, $2, $3, $4, $4, $5, $6, $7)
	`, id, userID, hash, now, expires, accountArg, mfaPending)
	if err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName(),
		Value:    raw,
		Path:     "/",
		MaxAge:   int(s.absoluteMaxAge.Seconds()),
		Secure:   s.secure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// CurrentAccountID reads the session's persisted current_account_id, if any.
// Returns uuid.Nil with nil error when the session has no account set.
func (s *Store) CurrentAccountID(ctx context.Context, r *http.Request) (uuid.UUID, error) {
	c, err := r.Cookie(s.cookieName())
	if err != nil {
		return uuid.Nil, ErrNoSession
	}
	hash := hashToken(c.Value)
	var id *uuid.UUID
	if err := s.pool.QueryRow(ctx, `
		SELECT current_account_id FROM sessions WHERE token_hash = $1
	`, hash).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrNoSession
		}
		return uuid.Nil, err
	}
	if id == nil {
		return uuid.Nil, nil
	}
	return *id, nil
}

// SetCurrentAccount persists current_account_id onto the session. The
// caller MUST have verified the user is a member of the account first.
func (s *Store) SetCurrentAccount(ctx context.Context, r *http.Request, accountID uuid.UUID) error {
	c, err := r.Cookie(s.cookieName())
	if err != nil {
		return ErrNoSession
	}
	hash := hashToken(c.Value)
	var arg any
	if accountID != uuid.Nil {
		arg = accountID
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE sessions SET current_account_id = $2 WHERE token_hash = $1
	`, hash, arg)
	return err
}

// MarkStaffVerified stamps the current session as having just completed an
// SSO step-up. The caller MUST have verified the SSO identity belongs to a
// staff user first.
func (s *Store) MarkStaffVerified(ctx context.Context, r *http.Request) error {
	c, err := r.Cookie(s.cookieName())
	if err != nil {
		return ErrNoSession
	}
	hash := hashToken(c.Value)
	_, err = s.pool.Exec(ctx, `
		UPDATE sessions SET staff_verified_at = now() WHERE token_hash = $1
	`, hash)
	return err
}

// Lookup resolves the session cookie to a user, sliding the idle window.
// Returns ErrNoSession when missing/expired/unknown.
func (s *Store) Lookup(ctx context.Context, r *http.Request) (*User, *Session, error) {
	c, err := r.Cookie(s.cookieName())
	if err != nil {
		return nil, nil, ErrNoSession
	}
	hash := hashToken(c.Value)
	now := time.Now().UTC()
	idleCutoff := now.Add(-s.idleTimeout)

	row := s.pool.QueryRow(ctx, `
		UPDATE sessions
		   SET last_seen_at = $1
		 WHERE token_hash   = $2
		   AND expires_at   > $1
		   AND last_seen_at > $3
	  RETURNING id, user_id, created_at, expires_at, impersonator_user_id, mfa_pending, staff_verified_at
	`, now, hash, idleCutoff)

	var sess Session
	if err := row.Scan(&sess.ID, &sess.UserID, &sess.CreatedAt, &sess.ExpiresAt,
		&sess.ImpersonatorID, &sess.MFAPending, &sess.StaffVerifiedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrNoSession
		}
		return nil, nil, err
	}

	u, err := s.loadUser(ctx, sess.UserID)
	if err != nil {
		return nil, nil, err
	}
	return u, &sess, nil
}

// loadUser fetches a non-deleted user by ID, returning ErrNoSession when the
// row is gone (the session points at a deleted user).
func (s *Store) loadUser(ctx context.Context, id uuid.UUID) (*User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `
		SELECT id, email, email_verified, is_staff, mfa_enabled, created_at
		  FROM users
		 WHERE id = $1 AND deleted_at IS NULL
	`, id).Scan(&u.ID, &u.Email, &u.EmailVerified, &u.IsStaff, &u.MFAEnabled, &u.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNoSession
		}
		return nil, err
	}
	return &u, nil
}

// LoadUserByID is the exported lookup used to resolve an impersonator's
// identity for the UI banner.
func (s *Store) LoadUserByID(ctx context.Context, id uuid.UUID) (*User, error) {
	return s.loadUser(ctx, id)
}

// StartImpersonation makes the current session act as targetUserID. The
// caller must have verified the real user is staff. The real user becomes
// the session's impersonator; current_account_id is cleared so the account
// middleware re-resolves it for the target.
func (s *Store) StartImpersonation(ctx context.Context, r *http.Request, staffUserID, targetUserID uuid.UUID) error {
	c, err := r.Cookie(s.cookieName())
	if err != nil {
		return ErrNoSession
	}
	hash := hashToken(c.Value)
	tag, err := s.pool.Exec(ctx, `
		UPDATE sessions
		   SET user_id = $2, impersonator_user_id = $3, current_account_id = NULL
		 WHERE token_hash = $1 AND impersonator_user_id IS NULL
	`, hash, targetUserID, staffUserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("auth: session already impersonating or not found")
	}
	return nil
}

// StopImpersonation restores the session to the impersonator (staff) user.
func (s *Store) StopImpersonation(ctx context.Context, r *http.Request) error {
	c, err := r.Cookie(s.cookieName())
	if err != nil {
		return ErrNoSession
	}
	hash := hashToken(c.Value)
	tag, err := s.pool.Exec(ctx, `
		UPDATE sessions
		   SET user_id = impersonator_user_id,
		       impersonator_user_id = NULL,
		       current_account_id = NULL
		 WHERE token_hash = $1 AND impersonator_user_id IS NOT NULL
	`, hash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("auth: session is not impersonating")
	}
	return nil
}

// Destroy revokes the current session and clears the cookie.
func (s *Store) Destroy(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie(s.cookieName()); err == nil {
		hash := hashToken(c.Value)
		if _, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, hash); err != nil {
			return err
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName(),
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Secure:   s.secure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func generateToken() (raw, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, hashToken(raw), nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return base64.RawStdEncoding.EncodeToString(sum[:])
}
