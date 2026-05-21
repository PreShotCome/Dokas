package auth

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// LoginThrottle records login attempts and locks an email out after too many
// recent failures. It's a brute-force speed bump, not a hard ban: the lock
// is a rolling window, and a successful login (which can only happen with the
// right password) clears the streak.
type LoginThrottle struct {
	pool     *pgxpool.Pool
	maxFails int
	window   time.Duration
}

// NewLoginThrottle returns a throttle that locks an email after maxFails
// failed attempts within window.
func NewLoginThrottle(pool *pgxpool.Pool, maxFails int, window time.Duration) *LoginThrottle {
	return &LoginThrottle{pool: pool, maxFails: maxFails, window: window}
}

// LockState describes whether an email is currently locked out.
type LockState struct {
	Locked     bool
	RetryAfter time.Duration
}

// Check reports whether the email is currently locked. An email is locked
// when it has accumulated >= maxFails failures since its most recent success
// (or since the window start) within the window.
func (t *LoginThrottle) Check(ctx context.Context, email string) (LockState, error) {
	cutoff := time.Now().UTC().Add(-t.window)

	// Most recent successful login inside the window, if any — failures
	// before it don't count.
	var lastSuccess *time.Time
	if err := t.pool.QueryRow(ctx, `
		SELECT max(at) FROM login_attempts
		 WHERE email = $1 AND succeeded = TRUE AND at > $2
	`, email, cutoff).Scan(&lastSuccess); err != nil {
		return LockState{}, err
	}

	since := cutoff
	if lastSuccess != nil && lastSuccess.After(since) {
		since = *lastSuccess
	}

	var fails int
	var oldestFail *time.Time
	if err := t.pool.QueryRow(ctx, `
		SELECT count(*), min(at) FROM login_attempts
		 WHERE email = $1 AND succeeded = FALSE AND at > $2
	`, email, since).Scan(&fails, &oldestFail); err != nil {
		return LockState{}, err
	}

	if fails < t.maxFails {
		return LockState{Locked: false}, nil
	}
	// Locked until the oldest counted failure ages out of the window.
	retryAfter := time.Duration(0)
	if oldestFail != nil {
		retryAfter = time.Until(oldestFail.Add(t.window))
	}
	if retryAfter < 0 {
		retryAfter = 0
	}
	return LockState{Locked: true, RetryAfter: retryAfter}, nil
}

// Record stores an attempt. ip may be empty.
func (t *LoginThrottle) Record(ctx context.Context, email, ip string, succeeded bool) error {
	var ipArg any
	if ip != "" {
		ipArg = ip
	}
	_, err := t.pool.Exec(ctx, `
		INSERT INTO login_attempts (email, ip, succeeded) VALUES ($1, $2, $3)
	`, email, ipArg, succeeded)
	return err
}

// Prune deletes attempts older than the retention horizon. Call periodically.
func (t *LoginThrottle) Prune(ctx context.Context, olderThan time.Duration) error {
	_, err := t.pool.Exec(ctx, `
		DELETE FROM login_attempts WHERE at < $1
	`, time.Now().UTC().Add(-olderThan))
	return err
}
