// Package heartbeat holds the backup check-in domain: monitors that expect a
// periodic ping from a customer's backup job, the public ping ingest, and the
// sweeper that flips a monitor to "down" when a ping is overdue.
//
// It rides on the same infrastructure the drill side uses — account scoping, a
// River periodic job (the sweeper), the webhook dispatcher, and the audit log
// — so the only genuinely new surface is the unauthenticated ping endpoint.
package heartbeat

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Status string

const (
	StatusNew    Status = "new"    // created, awaiting its first ping
	StatusUp     Status = "up"     // last ping arrived on time
	StatusDown   Status = "down"   // a ping is overdue (or an explicit fail)
	StatusPaused Status = "paused" // monitoring suspended by the owner
)

// PingKind enumerates the three ingest signals a cron can send.
type PingKind string

const (
	KindPing  PingKind = "ping"  // success — (re)arms the monitor to "up"
	KindStart PingKind = "start" // job started — recorded, no state change
	KindFail  PingKind = "fail"  // explicit failure — flips to "down" now
)

func validKind(k PingKind) bool {
	return k == KindPing || k == KindStart || k == KindFail
}

type Heartbeat struct {
	ID              uuid.UUID
	AccountID       uuid.UUID
	CreatedByUserID uuid.UUID
	Name            string
	Slug            string
	PingToken       string
	PeriodSeconds   int
	GraceSeconds    int
	Status          Status
	LastPingAt      *time.Time
	ExpectedBy      *time.Time
	CreatedAt       time.Time
}

// Overdue reports whether an active monitor is past its deadline as of now.
// Used by templates for an at-a-glance "should be up but isn't quite caught by
// the sweeper yet" hint; the authoritative flip happens in MarkOverdueDown.
func (h Heartbeat) Overdue(now time.Time) bool {
	if h.ExpectedBy == nil || (h.Status != StatusNew && h.Status != StatusUp) {
		return false
	}
	deadline := h.ExpectedBy.Add(time.Duration(h.GraceSeconds) * time.Second)
	return now.After(deadline)
}

type Ping struct {
	ID         uuid.UUID
	ReceivedAt time.Time
	Kind       PingKind
	SourceIP   string
	UserAgent  string
}

var ErrNotFound = errors.New("heartbeat: not found")

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Create registers a new monitor. It generates the secret ping token and arms
// the first deadline at now + period so a monitor that never pings still
// lapses to "down".
func (s *Store) Create(ctx context.Context, h Heartbeat) (Heartbeat, error) {
	h.PingToken = newToken()
	h.Slug = slugify(h.Name)
	h.Status = StatusNew
	err := s.pool.QueryRow(ctx, `
		INSERT INTO heartbeats
		    (account_id, created_by_user_id, name, slug, ping_token,
		     period_seconds, grace_seconds, status, expected_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'new',
		        now() + make_interval(secs => $6))
		RETURNING id, status, expected_by, created_at
	`, h.AccountID, h.CreatedByUserID, h.Name, h.Slug, h.PingToken,
		h.PeriodSeconds, h.GraceSeconds).
		Scan(&h.ID, &h.Status, &h.ExpectedBy, &h.CreatedAt)
	return h, err
}

func (s *Store) List(ctx context.Context, accountID uuid.UUID) ([]Heartbeat, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, account_id, created_by_user_id, name, slug, ping_token,
		       period_seconds, grace_seconds, status, last_ping_at, expected_by, created_at
		  FROM heartbeats
		 WHERE account_id = $1 AND deleted_at IS NULL
		 ORDER BY created_at DESC
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanHeartbeats(rows)
}

func (s *Store) Get(ctx context.Context, accountID, id uuid.UUID) (Heartbeat, error) {
	var h Heartbeat
	err := s.pool.QueryRow(ctx, `
		SELECT id, account_id, created_by_user_id, name, slug, ping_token,
		       period_seconds, grace_seconds, status, last_ping_at, expected_by, created_at
		  FROM heartbeats
		 WHERE id = $1 AND account_id = $2 AND deleted_at IS NULL
	`, id, accountID).Scan(&h.ID, &h.AccountID, &h.CreatedByUserID, &h.Name, &h.Slug,
		&h.PingToken, &h.PeriodSeconds, &h.GraceSeconds, &h.Status, &h.LastPingAt, &h.ExpectedBy, &h.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Heartbeat{}, ErrNotFound
	}
	return h, err
}

// RecordPing is the heart of the public ingest. It looks the monitor up by its
// secret token (the token *is* the auth), records the ping in the event log,
// and transitions status in a single transaction. It reports whether the call
// caused an up/down transition (so the caller only dispatches a webhook on a
// real edge, never on every routine ping).
//
//   - ping  → status "up", deadline re-armed to now + period.
//   - start → recorded only; status and deadline untouched.
//   - fail  → status "down" immediately.
//
// A ping or fail on a paused monitor resumes it (matches the mental model of
// "the cron is alive again"). Returns transitioned=true only when the visible
// up/down state actually changed.
func (s *Store) RecordPing(ctx context.Context, token string, kind PingKind, ip, ua string) (hb Heartbeat, transitioned bool, err error) {
	if !validKind(kind) {
		return Heartbeat{}, false, errors.New("heartbeat: invalid ping kind")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Heartbeat{}, false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var h Heartbeat
	err = tx.QueryRow(ctx, `
		SELECT id, account_id, created_by_user_id, name, slug, ping_token,
		       period_seconds, grace_seconds, status, last_ping_at, expected_by, created_at
		  FROM heartbeats
		 WHERE ping_token = $1 AND deleted_at IS NULL
		 FOR UPDATE
	`, token).Scan(&h.ID, &h.AccountID, &h.CreatedByUserID, &h.Name, &h.Slug,
		&h.PingToken, &h.PeriodSeconds, &h.GraceSeconds, &h.Status, &h.LastPingAt, &h.ExpectedBy, &h.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Heartbeat{}, false, ErrNotFound
	}
	if err != nil {
		return Heartbeat{}, false, err
	}

	if _, err = tx.Exec(ctx, `
		INSERT INTO heartbeat_pings (heartbeat_id, kind, source_ip, user_agent)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''))
	`, h.ID, kind, ip, ua); err != nil {
		return Heartbeat{}, false, err
	}

	prev := h.Status
	var newStatus Status
	rearm := false
	switch kind {
	case KindStart:
		// A "job started" signal is informational. Leave status and the
		// deadline alone — the matching success ping is what proves health.
		if err = tx.Commit(ctx); err != nil {
			return Heartbeat{}, false, err
		}
		return h, false, nil
	case KindFail:
		newStatus = StatusDown
	default: // KindPing
		newStatus = StatusUp
		rearm = true
	}

	// Re-arm the deadline on success; on an explicit fail keep the existing
	// deadline so a later success can still be judged on time.
	if rearm {
		err = tx.QueryRow(ctx, `
			UPDATE heartbeats
			   SET status = $2, last_ping_at = now(),
			       expected_by = now() + make_interval(secs => period_seconds)
			 WHERE id = $1
			RETURNING status, last_ping_at, expected_by
		`, h.ID, newStatus).Scan(&h.Status, &h.LastPingAt, &h.ExpectedBy)
	} else {
		err = tx.QueryRow(ctx, `
			UPDATE heartbeats
			   SET status = $2, last_ping_at = now()
			 WHERE id = $1
			RETURNING status, last_ping_at, expected_by
		`, h.ID, newStatus).Scan(&h.Status, &h.LastPingAt, &h.ExpectedBy)
	}
	if err != nil {
		return Heartbeat{}, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Heartbeat{}, false, err
	}

	// A transition is any change between the visible up/down states. new→up
	// counts (first contact); paused→up counts (resumed).
	transitioned = prev != newStatus
	return h, transitioned, nil
}

// MarkOverdueDown atomically flips every active monitor whose deadline (plus
// grace) has passed to "down" and returns the rows it flipped. The flip is the
// query, so two concurrent sweepers cannot both alert on the same outage: the
// second sees status already 'down' and its WHERE excludes the row. Lapsed-
// trial and soft-deleted accounts are skipped, matching the drill scheduler.
func (s *Store) MarkOverdueDown(ctx context.Context) ([]Heartbeat, error) {
	rows, err := s.pool.Query(ctx, `
		UPDATE heartbeats h
		   SET status = 'down'
		  FROM accounts a
		 WHERE h.account_id = a.id
		   AND h.deleted_at IS NULL
		   AND a.deleted_at IS NULL
		   AND h.status IN ('new', 'up')
		   AND h.expected_by IS NOT NULL
		   AND h.expected_by + make_interval(secs => h.grace_seconds) < now()
		   AND NOT (a.plan = 'trial' AND a.trial_ends_at IS NOT NULL AND a.trial_ends_at < now())
		RETURNING h.id, h.account_id, h.created_by_user_id, h.name, h.slug, h.ping_token,
		          h.period_seconds, h.grace_seconds, h.status, h.last_ping_at, h.expected_by, h.created_at
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanHeartbeats(rows)
}

// Pause suspends monitoring: status "paused", deadline cleared so the sweeper
// skips it. A later ping resumes it.
func (s *Store) Pause(ctx context.Context, accountID, id uuid.UUID) error {
	return s.exec1(ctx, `
		UPDATE heartbeats SET status = 'paused', expected_by = NULL
		 WHERE id = $1 AND account_id = $2 AND deleted_at IS NULL
	`, id, accountID)
}

// Resume re-arms a paused monitor: status "new", deadline at now + period.
func (s *Store) Resume(ctx context.Context, accountID, id uuid.UUID) error {
	return s.exec1(ctx, `
		UPDATE heartbeats
		   SET status = 'new', expected_by = now() + make_interval(secs => period_seconds)
		 WHERE id = $1 AND account_id = $2 AND deleted_at IS NULL
	`, id, accountID)
}

// Delete soft-deletes a monitor. The ping endpoint stops resolving its token
// (the WHERE deleted_at IS NULL guards every lookup).
func (s *Store) Delete(ctx context.Context, accountID, id uuid.UUID) error {
	return s.exec1(ctx, `
		UPDATE heartbeats SET deleted_at = now()
		 WHERE id = $1 AND account_id = $2 AND deleted_at IS NULL
	`, id, accountID)
}

func (s *Store) ListPings(ctx context.Context, heartbeatID uuid.UUID, limit int) ([]Ping, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, received_at, kind, COALESCE(source_ip, ''), COALESCE(user_agent, '')
		  FROM heartbeat_pings
		 WHERE heartbeat_id = $1
		 ORDER BY received_at DESC
		 LIMIT $2
	`, heartbeatID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Ping
	for rows.Next() {
		var p Ping
		if err := rows.Scan(&p.ID, &p.ReceivedAt, &p.Kind, &p.SourceIP, &p.UserAgent); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PrunePings deletes ping log rows older than the retention window. Wired into
// the compliance retention sweeper.
func (s *Store) PrunePings(ctx context.Context, retention time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM heartbeat_pings WHERE received_at < $1
	`, time.Now().UTC().Add(-retention))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// exec1 runs a single-row UPDATE and maps "nothing updated" to ErrNotFound.
func (s *Store) exec1(ctx context.Context, sql string, args ...any) error {
	tag, err := s.pool.Exec(ctx, sql, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func scanHeartbeats(rows pgx.Rows) ([]Heartbeat, error) {
	var out []Heartbeat
	for rows.Next() {
		var h Heartbeat
		if err := rows.Scan(&h.ID, &h.AccountID, &h.CreatedByUserID, &h.Name, &h.Slug,
			&h.PingToken, &h.PeriodSeconds, &h.GraceSeconds, &h.Status,
			&h.LastPingAt, &h.ExpectedBy, &h.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func newToken() string {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		panic("heartbeat: cannot read random bytes: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

var slugNonWord = regexp.MustCompile(`[^a-z0-9]+`)

// slugify makes a display-friendly slug from a name. Not unique-constrained —
// the token identifies a monitor; the slug is cosmetic.
func slugify(name string) string {
	s := slugNonWord.ReplaceAllString(strings.ToLower(name), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "monitor"
	}
	if len(s) > 60 {
		s = strings.Trim(s[:60], "-")
	}
	return s
}
