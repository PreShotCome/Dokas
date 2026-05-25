package assertions

import (
	"context"
	"testing"
)

// scanRow is a one-column result that copies a preset value into the Scan
// destination — enough to exercise every assertion kind without a database.
type scanRow struct{ val any }

func (r scanRow) Scan(dest ...any) error {
	switch d := dest[0].(type) {
	case *int64:
		*d = r.val.(int64)
	case *bool:
		*d = r.val.(bool)
	}
	return nil
}

type fakeQuerier struct {
	val  any
	rows Rows // optional, for sql_query
}

func (q fakeQuerier) QueryRow(_ context.Context, _ string, _ ...any) interface {
	Scan(dest ...any) error
} {
	return scanRow{val: q.val}
}

func (q fakeQuerier) Query(_ context.Context, _ string, _ ...any) (Rows, error) {
	return q.rows, nil
}

// fakeRows feeds sql_query a fixed sequence of values.
type fakeRows struct {
	cols  []string
	data  [][]any
	idx   int
}

func (r *fakeRows) Next() bool {
	if r.idx >= len(r.data) {
		return false
	}
	r.idx++
	return true
}
func (r *fakeRows) Values() ([]any, error) { return r.data[r.idx-1], nil }
func (r *fakeRows) FieldDescriptions() []FieldDescription {
	out := make([]FieldDescription, len(r.cols))
	for i, c := range r.cols {
		out[i] = FieldDescription{Name: c}
	}
	return out
}
func (r *fakeRows) Err() error { return nil }
func (r *fakeRows) Close()     {}

func TestRunKinds(t *testing.T) {
	cases := []struct {
		name string
		val  any
		spec Spec
		want bool
	}{
		{"row_count pass", int64(5),
			Spec{Kind: KindRowCount, Config: map[string]any{"table": "events", "min_rows": 3}}, true},
		{"row_count fail", int64(2),
			Spec{Kind: KindRowCount, Config: map[string]any{"table": "events", "min_rows": 3}}, false},
		{"table_exists pass", true,
			Spec{Kind: KindTableExists, Config: map[string]any{"table": "events"}}, true},
		{"table_exists fail", false,
			Spec{Kind: KindTableExists, Config: map[string]any{"table": "events"}}, false},
		{"column_exists pass", true,
			Spec{Kind: KindColumnExists, Config: map[string]any{"table": "events", "column": "id"}}, true},
		{"no_nulls pass", int64(0),
			Spec{Kind: KindNoNulls, Config: map[string]any{"table": "events", "column": "id"}}, true},
		{"no_nulls fail", int64(4),
			Spec{Kind: KindNoNulls, Config: map[string]any{"table": "events", "column": "id"}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Run(context.Background(), fakeQuerier{val: tc.val}, tc.spec)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if out.Passed != tc.want {
				t.Fatalf("passed = %v, want %v", out.Passed, tc.want)
			}
			if out.Kind != tc.spec.Kind {
				t.Fatalf("kind = %q, want %q", out.Kind, tc.spec.Kind)
			}
		})
	}
}

func TestRunRejectsBadInput(t *testing.T) {
	if _, err := Run(context.Background(), fakeQuerier{}, Spec{Kind: "bogus"}); err == nil {
		t.Fatal("unknown kind should error")
	}
	if _, err := Run(context.Background(), fakeQuerier{val: int64(0)},
		Spec{Kind: KindRowCount, Config: map[string]any{"table": "drop table foo"}}); err == nil {
		t.Fatal("invalid table identifier should error")
	}
}

func TestRunSQLQueryCapturesRows(t *testing.T) {
	q := fakeQuerier{rows: &fakeRows{
		cols: []string{"id", "email"},
		data: [][]any{
			{int64(1), "alice@example.com"},
			{int64(2), "bob@example.com"},
		},
	}}
	spec := Spec{Kind: KindSQLQuery, Config: map[string]any{
		"query":         "SELECT id, email FROM users",
		"expected_rows": 2,
	}}
	out, err := Run(context.Background(), q, spec)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !out.Passed {
		t.Fatalf("expected pass, got fail; actual=%s", out.Actual)
	}
	// Evidence must capture the query AND the actual rows.
	if !contains(string(out.Expected), "SELECT id, email FROM users") {
		t.Fatalf("Expected missing the query: %s", out.Expected)
	}
	if !contains(string(out.Actual), "alice@example.com") {
		t.Fatalf("Actual missing the captured row: %s", out.Actual)
	}
}

func TestSQLQueryRejectsMutations(t *testing.T) {
	rejected := []string{
		"DELETE FROM users",
		"UPDATE users SET email = 'x'",
		"INSERT INTO users VALUES (1)",
		"DROP TABLE users",
		"SELECT 1; DELETE FROM users",
		"WITH x AS (DELETE FROM users RETURNING id) SELECT * FROM x",
	}
	for _, q := range rejected {
		if err := ValidateConfig(KindSQLQuery, map[string]any{"query": q}); err == nil {
			t.Errorf("ValidateConfig should reject %q", q)
		}
	}
	accepted := []string{
		"SELECT count(*) FROM users",
		"select * from orders;",
		"WITH recent AS (SELECT * FROM orders WHERE created_at > now() - interval '1 day') SELECT count(*) FROM recent",
	}
	for _, q := range accepted {
		if err := ValidateConfig(KindSQLQuery, map[string]any{"query": q}); err != nil {
			t.Errorf("ValidateConfig should accept %q: %v", q, err)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestValidIdent(t *testing.T) {
	good := []string{"events", "user_id", "_t", "T1"}
	bad := []string{"", "1abc", "a-b", "a.b", "select*", "drop table"}
	for _, s := range good {
		if !ValidIdent(s) {
			t.Errorf("ValidIdent(%q) = false, want true", s)
		}
	}
	for _, s := range bad {
		if ValidIdent(s) {
			t.Errorf("ValidIdent(%q) = true, want false", s)
		}
	}
}

func TestValidateConfig(t *testing.T) {
	ok := []struct {
		kind string
		cfg  map[string]any
	}{
		{KindRowCount, map[string]any{"table": "events", "min_rows": 1}},
		{KindTableExists, map[string]any{"table": "events"}},
		{KindColumnExists, map[string]any{"table": "events", "column": "id"}},
		{KindNoNulls, map[string]any{"table": "events", "column": "id"}},
	}
	for _, c := range ok {
		if err := ValidateConfig(c.kind, c.cfg); err != nil {
			t.Errorf("ValidateConfig(%s) = %v, want nil", c.kind, err)
		}
	}
	bad := []struct {
		kind string
		cfg  map[string]any
	}{
		{"bogus", map[string]any{"table": "events"}},
		{KindRowCount, map[string]any{"table": ""}},
		{KindRowCount, map[string]any{"table": "events", "min_rows": -1}},
		{KindColumnExists, map[string]any{"table": "events"}},
	}
	for _, c := range bad {
		if err := ValidateConfig(c.kind, c.cfg); err == nil {
			t.Errorf("ValidateConfig(%s, %v) = nil, want error", c.kind, c.cfg)
		}
	}
}
