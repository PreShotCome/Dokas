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
	KindSQLQuery     = "sql_query"
)

// maxSQLQueryRows caps how many rows a sql_query assertion captures into
// evidence. Beyond this we truncate and flag — keeps the signed PDF a
// readable artifact, not a database export.
const maxSQLQueryRows = 100

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
	// Query is used by sql_query to capture multi-row results into
	// evidence. Returns the rows, column names, and any error. The
	// concrete implementation in steps.go wraps pgx.Rows.
	Query(ctx context.Context, sql string, args ...any) (Rows, error)
}

// Rows is the minimal slice of pgx.Rows sql_query needs to materialise
// captured-result evidence.
type Rows interface {
	Next() bool
	Values() ([]any, error)
	FieldDescriptions() []FieldDescription
	Err() error
	Close()
}

// FieldDescription is the column metadata sql_query records — just the
// column name, kept narrow on purpose.
type FieldDescription struct {
	Name string
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
	case KindSQLQuery:
		return runSQLQuery(ctx, conn, s.Config)
	default:
		return Outcome{}, fmt.Errorf("assertions: unknown kind %q", s.Kind)
	}
}

// runSQLQuery executes a customer-supplied SQL statement against the
// sandbox and captures the **query string AND the full result rows** as
// evidence. The customer no longer trusts our assertion logic — they
// wrote the SQL, they read the actual values in the signed PDF.
//
// Pass criterion: if config["expected_rows"] is set, the result row
// count must equal it. Otherwise the assertion passes as long as the
// query executed without error (a smoke-test "did it run").
//
// Safety: ValidateConfig enforces read-only-ish SQL at the form
// boundary (parsed for INSERT/UPDATE/DELETE/DROP/etc.). Even so the
// sandbox is ephemeral and is torn down after the drill, so a mutation
// would only affect the throw-away copy — but a customer-readable
// assertion that mutates would be confusing, so we reject it up front.
func runSQLQuery(ctx context.Context, conn Querier, cfg map[string]any) (Outcome, error) {
	query, _ := cfg["query"].(string)
	if query == "" {
		return Outcome{}, errors.New("sql_query: missing 'query'")
	}
	if err := validateReadOnlySQL(query); err != nil {
		return Outcome{}, fmt.Errorf("sql_query: %w", err)
	}
	rows, err := conn.Query(ctx, query)
	if err != nil {
		return Outcome{}, fmt.Errorf("sql_query: %w", err)
	}
	defer rows.Close()

	var cols []string
	for _, fd := range rows.FieldDescriptions() {
		cols = append(cols, fd.Name)
	}
	var (
		captured  []map[string]any
		total     int
		truncated bool
	)
	for rows.Next() {
		total++
		if total > maxSQLQueryRows {
			truncated = true
			continue
		}
		vals, vErr := rows.Values()
		if vErr != nil {
			return Outcome{}, fmt.Errorf("sql_query: read row: %w", vErr)
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			if i < len(vals) {
				row[c] = vals[i]
			}
		}
		captured = append(captured, row)
	}
	if err := rows.Err(); err != nil {
		return Outcome{}, fmt.Errorf("sql_query: %w", err)
	}

	expected := map[string]any{"query": query}
	actual := map[string]any{
		"columns":    cols,
		"row_count":  total,
		"rows":       captured,
		"truncated":  truncated,
		"row_cap":    maxSQLQueryRows,
	}

	passed := true
	if v, ok := cfg["expected_rows"]; ok {
		want := intFrom(v)
		expected["expected_rows"] = want
		passed = total == want
	}
	return outcome(KindSQLQuery, expected, actual, passed)
}

// validateReadOnlySQL rejects statements that would mutate the sandbox.
// Conservative keyword-prefix scan — the sandbox is throw-away so a slip
// only affects our temporary copy, but a customer-readable assertion that
// mutates would be a foot-gun in evidence. CTEs are allowed (WITH ...
// SELECT). Multiple statements are rejected outright.
func validateReadOnlySQL(q string) error {
	if hasMultipleStatements(q) {
		return errors.New("only a single SELECT (or WITH ... SELECT) is allowed")
	}
	upper := upperTrim(q)
	if !startsWithAny(upper, "SELECT", "WITH ", "EXPLAIN", "SHOW", "VALUES") {
		return errors.New("only read-only statements are allowed (SELECT, WITH, EXPLAIN, SHOW, VALUES)")
	}
	for _, kw := range forbiddenKeywords {
		if containsWord(upper, kw) {
			return fmt.Errorf("query contains forbidden keyword %q", kw)
		}
	}
	return nil
}

// forbiddenKeywords are tokens whose presence anywhere in the query —
// even inside a CTE — disqualifies it. Belt-and-braces; the prefix
// check already rejects the obvious cases.
var forbiddenKeywords = []string{
	"INSERT", "UPDATE", "DELETE", "TRUNCATE",
	"DROP", "CREATE", "ALTER", "GRANT", "REVOKE",
	"COPY", "MERGE", "CALL",
}

func intFrom(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case int64:
		return int(x)
	default:
		return 0
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
	case KindRowCount, KindTableExists, KindColumnExists, KindNoNulls, KindSQLQuery:
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
	// sql_query carries a raw query string instead of an identifier —
	// validate it separately and short-circuit.
	if kind == KindSQLQuery {
		q, _ := config["query"].(string)
		if q == "" {
			return errors.New("assertions: sql_query requires a 'query' string")
		}
		if err := validateReadOnlySQL(q); err != nil {
			return fmt.Errorf("assertions: sql_query: %w", err)
		}
		return nil
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

// hasMultipleStatements rejects strings containing a non-trailing semicolon.
// Trailing whitespace + a single semicolon is allowed (clients often append it).
func hasMultipleStatements(q string) bool {
	body := q
	// Trim trailing whitespace + at most one trailing semicolon.
	for len(body) > 0 {
		last := body[len(body)-1]
		if last == ' ' || last == '\n' || last == '\t' || last == '\r' {
			body = body[:len(body)-1]
			continue
		}
		break
	}
	if len(body) > 0 && body[len(body)-1] == ';' {
		body = body[:len(body)-1]
	}
	for i := 0; i < len(body); i++ {
		if body[i] == ';' {
			return true
		}
	}
	return false
}

func upperTrim(s string) string {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		if r == ' ' || r == '\n' || r == '\t' || r == '\r' {
			if len(out) == 0 {
				continue // skip leading whitespace
			}
		}
		if r >= 'a' && r <= 'z' {
			out = append(out, byte(r-32))
			continue
		}
		if r < 128 {
			out = append(out, byte(r))
		}
	}
	return string(out)
}

func startsWithAny(upper string, prefixes ...string) bool {
	for _, p := range prefixes {
		if len(upper) >= len(p) && upper[:len(p)] == p {
			return true
		}
	}
	return false
}

// containsWord returns true when upper contains kw as a whole word —
// surrounded by non-letter characters (or the string boundary). Prevents
// false positives like "SELECTED" matching "SELECT".
func containsWord(upper, kw string) bool {
	for i := 0; i+len(kw) <= len(upper); i++ {
		if upper[i:i+len(kw)] != kw {
			continue
		}
		if i > 0 && isIdentByte(upper[i-1]) {
			continue
		}
		if i+len(kw) < len(upper) && isIdentByte(upper[i+len(kw)]) {
			continue
		}
		return true
	}
	return false
}

func isIdentByte(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}
