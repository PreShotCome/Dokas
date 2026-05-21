// Package assertions defines the checks a drill runs against a restored
// sandbox database. Each kind is a small, parameterised SQL query; the
// drill's assert step runs every assertion configured on its target.
package assertions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Kinds.
const (
	KindRowCount     = "row_count"
	KindTableExists  = "table_exists"
	KindColumnExists = "column_exists"
	KindNoNulls      = "no_nulls"
)

// Spec is one configured assertion: a kind plus its JSON config.
type Spec struct {
	Kind   string
	Config map[string]any
}

// Outcome is the result of running an assertion — JSON-encodable expected
// and actual blobs (stored in assertion_results) plus pass/fail.
type Outcome struct {
	Kind     string
	Expected []byte
	Actual   []byte
	Passed   bool
}

// Querier is the slice of *pgx.Conn the assertions need. Defining it as an
// interface keeps the package decoupled from a concrete connection type.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) interface {
		Scan(dest ...any) error
	}
}

// Run executes one assertion against the sandbox connection.
func Run(ctx context.Context, conn Querier, s Spec) (Outcome, error) {
	switch s.Kind {
	case KindRowCount:
		return runRowCount(ctx, conn, s.Config)
	case KindTableExists:
		return runTableExists(ctx, conn, s.Config)
	case KindColumnExists:
		return runColumnExists(ctx, conn, s.Config)
	case KindNoNulls:
		return runNoNulls(ctx, conn, s.Config)
	default:
		return Outcome{}, fmt.Errorf("assertions: unknown kind %q", s.Kind)
	}
}

func runRowCount(ctx context.Context, conn Querier, cfg map[string]any) (Outcome, error) {
	table, err := ident(cfg, "table")
	if err != nil {
		return Outcome{}, err
	}
	minRows := intVal(cfg, "min_rows")

	var n int64
	if err := conn.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %q`, table)).Scan(&n); err != nil {
		return Outcome{}, fmt.Errorf("row_count: %w", err)
	}
	return outcome(KindRowCount,
		map[string]any{"table": table, "min_rows": minRows},
		map[string]any{"table": table, "rows": n},
		n >= int64(minRows))
}

func runTableExists(ctx context.Context, conn Querier, cfg map[string]any) (Outcome, error) {
	table, err := ident(cfg, "table")
	if err != nil {
		return Outcome{}, err
	}
	var exists bool
	if err := conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			 WHERE table_schema = 'public' AND table_name = $1
		)`, table).Scan(&exists); err != nil {
		return Outcome{}, fmt.Errorf("table_exists: %w", err)
	}
	return outcome(KindTableExists,
		map[string]any{"table": table},
		map[string]any{"table": table, "exists": exists},
		exists)
}

func runColumnExists(ctx context.Context, conn Querier, cfg map[string]any) (Outcome, error) {
	table, err := ident(cfg, "table")
	if err != nil {
		return Outcome{}, err
	}
	column, err := ident(cfg, "column")
	if err != nil {
		return Outcome{}, err
	}
	var exists bool
	if err := conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			 WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
		)`, table, column).Scan(&exists); err != nil {
		return Outcome{}, fmt.Errorf("column_exists: %w", err)
	}
	return outcome(KindColumnExists,
		map[string]any{"table": table, "column": column},
		map[string]any{"table": table, "column": column, "exists": exists},
		exists)
}

func runNoNulls(ctx context.Context, conn Querier, cfg map[string]any) (Outcome, error) {
	table, err := ident(cfg, "table")
	if err != nil {
		return Outcome{}, err
	}
	column, err := ident(cfg, "column")
	if err != nil {
		return Outcome{}, err
	}
	var nulls int64
	if err := conn.QueryRow(ctx,
		fmt.Sprintf(`SELECT count(*) FROM %q WHERE %q IS NULL`, table, column)).Scan(&nulls); err != nil {
		return Outcome{}, fmt.Errorf("no_nulls: %w", err)
	}
	return outcome(KindNoNulls,
		map[string]any{"table": table, "column": column, "max_nulls": 0},
		map[string]any{"table": table, "column": column, "nulls": nulls},
		nulls == 0)
}

func outcome(kind string, expected, actual map[string]any, passed bool) (Outcome, error) {
	exp, err := json.Marshal(expected)
	if err != nil {
		return Outcome{}, err
	}
	act, err := json.Marshal(actual)
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{Kind: kind, Expected: exp, Actual: act, Passed: passed}, nil
}

// ident pulls a config value and validates it as a SQL identifier — table
// and column names are interpolated into queries, so they must be a strict
// allowlist (Postgres can't bind identifiers as parameters).
func ident(cfg map[string]any, key string) (string, error) {
	v, _ := cfg[key].(string)
	if !validIdent(v) {
		return "", fmt.Errorf("assertions: %q is not a valid identifier: %q", key, v)
	}
	return v, nil
}

func intVal(cfg map[string]any, key string) int {
	switch v := cfg[key].(type) {
	case float64: // JSON numbers decode to float64
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	default:
		return 0
	}
}

// validIdent enforces a conservative identifier allowlist: letters, digits,
// underscore; no leading digit; max 63 chars (Postgres limit).
func validIdent(s string) bool {
	if s == "" || len(s) > 63 {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r == '_':
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// ValidIdent is the exported identifier check, used by handlers validating
// assertion config at the form boundary.
func ValidIdent(s string) bool { return validIdent(s) }

// ValidKind reports whether kind is a known assertion kind.
func ValidKind(kind string) bool {
	switch kind {
	case KindRowCount, KindTableExists, KindColumnExists, KindNoNulls:
		return true
	default:
		return false
	}
}

// ValidateConfig checks that config carries the keys a given kind needs and
// that identifier values are valid. Used at the form and API boundaries
// before an assertion is persisted, so a bad config never reaches a drill.
func ValidateConfig(kind string, config map[string]any) error {
	if !ValidKind(kind) {
		return fmt.Errorf("assertions: unknown kind %q", kind)
	}
	if _, err := ident(config, "table"); err != nil {
		return err
	}
	switch kind {
	case KindColumnExists, KindNoNulls:
		if _, err := ident(config, "column"); err != nil {
			return err
		}
	case KindRowCount:
		if intVal(config, "min_rows") < 0 {
			return errors.New("assertions: min_rows must be non-negative")
		}
	}
	return nil
}
