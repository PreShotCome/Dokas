package audit

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Logger struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Logger {
	return &Logger{pool: pool}
}

type Event struct {
	AccountID  *uuid.UUID
	ActorID    *uuid.UUID
	Action     string
	TargetKind string
	TargetID   string
	IP         string
	UserAgent  string
	Metadata   map[string]any
}

func (l *Logger) Record(ctx context.Context, e Event) error {
	meta := []byte(`{}`)
	if len(e.Metadata) > 0 {
		b, err := json.Marshal(e.Metadata)
		if err != nil {
			return err
		}
		meta = b
	}
	_, err := l.pool.Exec(ctx, `
		INSERT INTO audit_events
		    (account_id, actor_id, action, target_kind, target_id, ip, user_agent, metadata)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), $6, NULLIF($7, ''), $8::jsonb)
	`, e.AccountID, e.ActorID, e.Action, e.TargetKind, e.TargetID,
		ipOrNil(e.IP), e.UserAgent, meta)
	return err
}

// FromRequest is a convenience that pulls IP and UA off an http.Request.
func FromRequest(r *http.Request) (ip, ua string) {
	return ClientIP(r), r.UserAgent()
}

// ClientIP picks the most trustworthy IP from the request. In dev or behind
// a proxy without an XFF allowlist, prefer RemoteAddr.
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the leftmost, which is the original client per the spec.
		if i := strings.Index(xff, ","); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func ipOrNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Entry is one audit event as read back for display. Unlike Event (the write
// shape), the actor is resolved to an email where possible, and nullable
// columns come back as empty strings rather than pointers.
type Entry struct {
	ID         int64
	At         time.Time
	ActorEmail string // joined from users; empty if the actor is null or deleted
	Action     string
	TargetKind string
	TargetID   string
	IP         string
	Metadata   map[string]any
}

// ListForAccount returns an account's audit events newest-first, in pages.
// limit caps the page size; beforeID is a keyset cursor — pass 0 for the
// first page, then the ID of the last row to fetch the next, older page.
// One extra row beyond limit is never returned; callers detect "more" by
// requesting limit+1 themselves if they want a has-more flag.
func (l *Logger) ListForAccount(ctx context.Context, accountID uuid.UUID, limit int, beforeID int64) ([]Entry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := l.pool.Query(ctx, `
		SELECT e.id, e.at, COALESCE(u.email, ''), e.action,
		       COALESCE(e.target_kind, ''), COALESCE(e.target_id, ''),
		       COALESCE(host(e.ip), ''), e.metadata
		  FROM audit_events e
		  LEFT JOIN users u ON u.id = e.actor_id
		 WHERE e.account_id = $1
		   AND ($2 = 0 OR e.id < $2)
		 ORDER BY e.id DESC
		 LIMIT $3
	`, accountID, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Entry
	for rows.Next() {
		var e Entry
		var meta []byte
		if err := rows.Scan(&e.ID, &e.At, &e.ActorEmail, &e.Action,
			&e.TargetKind, &e.TargetID, &e.IP, &meta); err != nil {
			return nil, err
		}
		if len(meta) > 0 {
			_ = json.Unmarshal(meta, &e.Metadata)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// AlertActions are the audit actions surfaced as alerts in the responder app's
// feed: a drill finished (pass or fail) or a backup check-in changed liveness.
var AlertActions = []string{"drill.failed", "drill.completed", "heartbeat.down", "heartbeat.up"}

// ListAlertsForAccount is ListForAccount filtered to AlertActions — the feed
// the mobile app polls. Same newest-first keyset pagination (beforeID; pass 0
// for the first page).
func (l *Logger) ListAlertsForAccount(ctx context.Context, accountID uuid.UUID, limit int, beforeID int64) ([]Entry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := l.pool.Query(ctx, `
		SELECT e.id, e.at, COALESCE(u.email, ''), e.action,
		       COALESCE(e.target_kind, ''), COALESCE(e.target_id, ''),
		       COALESCE(host(e.ip), ''), e.metadata
		  FROM audit_events e
		  LEFT JOIN users u ON u.id = e.actor_id
		 WHERE e.account_id = $1
		   AND e.action = ANY($2)
		   AND ($3 = 0 OR e.id < $3)
		 ORDER BY e.id DESC
		 LIMIT $4
	`, accountID, AlertActions, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Entry
	for rows.Next() {
		var e Entry
		var meta []byte
		if err := rows.Scan(&e.ID, &e.At, &e.ActorEmail, &e.Action,
			&e.TargetKind, &e.TargetID, &e.IP, &meta); err != nil {
			return nil, err
		}
		if len(meta) > 0 {
			_ = json.Unmarshal(meta, &e.Metadata)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
