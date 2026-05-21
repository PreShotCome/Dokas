package audit

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"

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
