package drill

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ReadinessStat is the per-target drill aggregate the recovery-readiness score
// is computed from (see internal/readiness).
type ReadinessStat struct {
	LastSuccessAt *time.Time
	LastStatus    string // most recent terminal status ("succeeded"/"failed"), "" if none
	Recent        int    // terminal drills in the last 90 days
	RecentPassed  int    // of those, how many passed
}

// ReadinessStats returns drill aggregates for every target in the account that
// has at least one drill, keyed by target ID. One grouped query, so it scales
// with the number of targets, not drills.
func (s *Store) ReadinessStats(ctx context.Context, accountID uuid.UUID) (map[uuid.UUID]ReadinessStat, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			d.target_id,
			MAX(d.completed_at) FILTER (WHERE d.status = 'succeeded') AS last_success,
			COUNT(*) FILTER (WHERE d.status IN ('succeeded','failed')
				AND d.created_at > now() - interval '90 days') AS recent,
			COUNT(*) FILTER (WHERE d.status = 'succeeded'
				AND d.created_at > now() - interval '90 days') AS recent_passed,
			(ARRAY_AGG(d.status ORDER BY d.completed_at DESC NULLS LAST)
				FILTER (WHERE d.status IN ('succeeded','failed')))[1] AS last_status
		FROM drills d
		WHERE d.account_id = $1
		GROUP BY d.target_id
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[uuid.UUID]ReadinessStat)
	for rows.Next() {
		var id uuid.UUID
		var st ReadinessStat
		var lastStatus *string
		if err := rows.Scan(&id, &st.LastSuccessAt, &st.Recent, &st.RecentPassed, &lastStatus); err != nil {
			return nil, err
		}
		if lastStatus != nil {
			st.LastStatus = *lastStatus
		}
		out[id] = st
	}
	return out, rows.Err()
}
