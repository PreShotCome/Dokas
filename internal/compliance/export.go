// Package compliance implements the GDPR/CCPA data-rights surface: a full
// data export and a soft-delete + hard-delete (crypto-shred) lifecycle for
// accounts.
package compliance

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Exporter assembles the JSON data export for an account.
type Exporter struct{ pool *pgxpool.Pool }

func NewExporter(pool *pgxpool.Pool) *Exporter { return &Exporter{pool: pool} }

// Export streams a JSON document of everything held for an account: the
// account row, members, targets, drills (with steps + assertions), webhook
// endpoints + deliveries, and the account's audit trail.
func (e *Exporter) Export(ctx context.Context, accountID uuid.UUID, w io.Writer) error {
	doc := map[string]any{
		"export_generated_at": time.Now().UTC().Format(time.RFC3339),
		"account_id":          accountID.String(),
	}

	account, err := e.queryRows(ctx, `
		SELECT id, name, slug, plan, stripe_customer_id, created_at, deleted_at
		  FROM accounts WHERE id = $1
	`, accountID)
	if err != nil {
		return err
	}
	doc["account"] = first(account)

	sections := []struct {
		name string
		sql  string
	}{
		{"members", `
			SELECT m.user_id, u.email::text AS email, m.role, m.created_at
			  FROM memberships m JOIN users u ON u.id = m.user_id
			 WHERE m.account_id = $1 ORDER BY m.created_at`},
		{"database_targets", `
			SELECT id, name, source_kind, source_uri, assertion_table,
			       assertion_min_rows, created_by_user_id, created_at, deleted_at
			  FROM database_targets WHERE account_id = $1 ORDER BY created_at`},
		{"drills", `
			SELECT id, target_id, status, created_by_user_id, started_at,
			       completed_at, error, evidence_path, created_at
			  FROM drills WHERE account_id = $1 ORDER BY created_at`},
		{"drill_steps", `
			SELECT s.id, s.drill_id, s.name, s.status, s.started_at,
			       s.completed_at, s.error
			  FROM drill_steps s JOIN drills d ON d.id = s.drill_id
			 WHERE d.account_id = $1 ORDER BY s.drill_id, s.ordinal`},
		{"assertion_results", `
			SELECT a.id, a.drill_id, a.kind, a.expected, a.actual, a.passed, a.at
			  FROM assertion_results a JOIN drills d ON d.id = a.drill_id
			 WHERE d.account_id = $1 ORDER BY a.at`},
		{"evidence_signatures", `
			SELECT g.drill_id, g.algorithm, g.public_key_id, g.pdf_sha256,
			       g.signed_at, g.retain_until
			  FROM evidence_signatures g JOIN drills d ON d.id = g.drill_id
			 WHERE d.account_id = $1 ORDER BY g.signed_at`},
		{"webhook_endpoints", `
			SELECT id, url, enabled, created_at, deleted_at
			  FROM webhook_endpoints WHERE account_id = $1 ORDER BY created_at`},
		{"webhook_deliveries", `
			SELECT id, endpoint_id, event, status, attempt_count,
			       last_status_code, created_at, delivered_at
			  FROM webhook_deliveries WHERE account_id = $1 ORDER BY created_at`},
		{"audit_events", `
			SELECT id, at, actor_id, action, target_kind, target_id, metadata
			  FROM audit_events WHERE account_id = $1 ORDER BY at`},
	}
	for _, s := range sections {
		rows, err := e.queryRows(ctx, s.sql, accountID)
		if err != nil {
			return err
		}
		doc[s.name] = rows
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

// queryRows runs a query and returns each row as a map keyed by column name.
// Generic so the export doesn't need a struct per table.
func (e *Exporter) queryRows(ctx context.Context, sql string, args ...any) ([]map[string]any, error) {
	rows, err := e.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fields := rows.FieldDescriptions()
	var out []map[string]any
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		m := make(map[string]any, len(fields))
		for i, f := range fields {
			m[string(f.Name)] = normalizeValue(vals[i])
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// normalizeValue makes pgx values JSON-friendly (UUIDs, []byte JSON blobs).
func normalizeValue(v any) any {
	switch t := v.(type) {
	case [16]byte:
		return uuid.UUID(t).String()
	case []byte:
		// JSONB columns arrive as []byte; embed them as parsed JSON.
		var parsed any
		if json.Unmarshal(t, &parsed) == nil {
			return parsed
		}
		return string(t)
	default:
		return v
	}
}

func first(rows []map[string]any) any {
	if len(rows) == 0 {
		return nil
	}
	return rows[0]
}
