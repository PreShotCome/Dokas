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

type fakeQuerier struct{ val any }

func (q fakeQuerier) QueryRow(_ context.Context, _ string, _ ...any) interface {
	Scan(dest ...any) error
} {
	return scanRow{val: q.val}
}

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
