// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

package drill

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ListEvidenceDrills returns the account's drills that have signed evidence and
// completed on or after `since`, newest first — the input to the downloadable
// evidence bundle.
func (s *Store) ListEvidenceDrills(ctx context.Context, accountID uuid.UUID, since time.Time) ([]Drill, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, target_id, account_id, created_by_user_id, status, started_at, completed_at,
		       error, evidence_path, sandbox_db, source_hash, created_at
		  FROM drills
		 WHERE account_id = $1
		   AND evidence_path IS NOT NULL AND evidence_path <> ''
		   AND completed_at >= $2
		 ORDER BY completed_at DESC
	`, accountID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Drill
	for rows.Next() {
		var d Drill
		if err := rows.Scan(&d.ID, &d.TargetID, &d.AccountID, &d.CreatedByUserID, &d.Status,
			&d.StartedAt, &d.CompletedAt, &d.Error, &d.EvidencePath, &d.SandboxDB, &d.SourceHash, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
