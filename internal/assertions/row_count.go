// Package assertions defines the assertion kinds a drill can run against a
// restored sandbox. Phase 2 ships row_count only; more kinds (schema_match,
// pk_uniqueness, fk_integrity, table_size) come in later phases.
package assertions

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/preshotcome/anything/internal/runner"
)

// RowCountSpec is the input for a row_count assertion.
type RowCountSpec struct {
	Table   string `json:"table"`
	MinRows int    `json:"min_rows"`
}

// RowCountActual is the recorded outcome shape.
type RowCountActual struct {
	Table string `json:"table"`
	Rows  int64  `json:"rows"`
}

// RowCount executes the assertion against a sandbox via the runner and
// returns the result as JSON-encodable expected/actual blobs plus pass/fail.
func RowCount(ctx context.Context, r runner.Runner, sb *runner.Sandbox, spec RowCountSpec) (kind string, expected, actual []byte, passed bool, err error) {
	res, err := r.AssertRowCount(ctx, sb, runner.RowCountInput{Table: spec.Table, MinRows: spec.MinRows})
	if err != nil {
		return "row_count", nil, nil, false, fmt.Errorf("row_count: %w", err)
	}

	exp, err := json.Marshal(spec)
	if err != nil {
		return "row_count", nil, nil, false, err
	}
	rows, _ := res.Actual.(map[string]any)["rows"].(int64)
	act, err := json.Marshal(RowCountActual{Table: spec.Table, Rows: rows})
	if err != nil {
		return "row_count", nil, nil, false, err
	}
	return res.Kind, exp, act, res.Passed, nil
}
