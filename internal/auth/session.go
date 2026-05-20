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
	cookieNameSecure   = "__Host-rd_session"
	cookieNameInsecure = "rd_session"
)

var ErrNoSession = errors.New("no session")

type Session struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	CreatedAt time.Time
	ExpiresAt time.Time
}

type User struct {
	ID            uuid.UUID
	Email         string
	EmailVerified bool
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

// Create issues a fresh session for a user and writes the cookie.
// Returns the raw token (only sent to the client; only its hash is stored).
func (s *Store) Create(ctx context.Context, w http.ResponseWriter, userID uuid.UUID) error {
	raw, hash, err := generateToken()
	if err != nil {
		return err
	}
	id := uuid.New()
	now := time.Now().UTC()
	expires := now.Add(s.absoluteMaxAge)

	_, err = s.pool.Exec(ctx, `
		INSERT INTO sessions (id, user_id, token_hash, created_at, last_seen_at, expires_at)
		VALUES ($1, $2, $3, $4, $4, $5)
	`, id, userID, hash, now, expires)
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
	  RETURNING id, user_id, created_at, expires_at
	`, now, hash, idleCutoff)

	var sess Session
	if err := row.Scan(&sess.ID, &sess.UserID, &sess.CreatedAt, &sess.ExpiresAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrNoSession
		}
		return nil, nil, err
	}

	var u User
	err = s.pool.QueryRow(ctx, `
		SELECT id, email, email_verified, created_at
		  FROM users
		 WHERE id = $1 AND deleted_at IS NULL
	`, sess.UserID).Scan(&u.ID, &u.Email, &u.EmailVerified, &u.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrNoSession
		}
		return nil, nil, err
	}
	return &u, &sess, nil
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
