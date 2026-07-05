package account

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Team is a sub-group within an account (org). See docs/runbooks/teams.md and
// issue #29. Databases are assigned to a team; a member sees only their
// teams' databases (plus unassigned ones), while owners/admins see all.
type Team struct {
	ID        uuid.UUID
	AccountID uuid.UUID
	Name      string
}

// TeamMembershipRow is one (team, user) edge, used to render the roster.
type TeamMembershipRow struct {
	TeamID uuid.UUID
	UserID uuid.UUID
}

// ErrTeamNameTaken is returned when a team name already exists in the account.
var ErrTeamNameTaken = errors.New("account: a team with that name already exists")

// CreateTeam adds a team to the account. Names are unique per account
// (case-insensitive); a duplicate returns ErrTeamNameTaken.
func (s *Store) CreateTeam(ctx context.Context, accountID uuid.UUID, name string) (Team, error) {
	t := Team{AccountID: accountID, Name: name}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO teams (account_id, name) VALUES ($1, $2)
		RETURNING id
	`, accountID, name).Scan(&t.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return Team{}, ErrTeamNameTaken
		}
		return Team{}, err
	}
	return t, nil
}

// RenameTeam changes a team's name. Scoped to the account so one tenant can't
// rename another's team; a duplicate name returns ErrTeamNameTaken.
func (s *Store) RenameTeam(ctx context.Context, accountID, teamID uuid.UUID, name string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE teams SET name = $3
		 WHERE id = $1 AND account_id = $2 AND deleted_at IS NULL
	`, teamID, accountID, name)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrTeamNameTaken
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteTeam removes a team. The FKs do the cleanup: team_memberships CASCADE
// away and database_targets.team_id is SET NULL, so the team's databases fall
// back to account-wide rather than being orphaned or hidden.
func (s *Store) DeleteTeam(ctx context.Context, accountID, teamID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM teams WHERE id = $1 AND account_id = $2
	`, teamID, accountID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListTeams returns the account's teams, name-ordered.
func (s *Store) ListTeams(ctx context.Context, accountID uuid.UUID) ([]Team, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, account_id, name FROM teams
		 WHERE account_id = $1 AND deleted_at IS NULL
		 ORDER BY name
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Team
	for rows.Next() {
		var t Team
		if err := rows.Scan(&t.ID, &t.AccountID, &t.Name); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// AddTeamMember puts an account member on a team. Both the team and the user's
// org membership must belong to the account, so a request can't add an
// outsider or reference another tenant's team; either failure affects zero
// rows and returns ErrNotFound. Idempotent — re-adding is a no-op.
func (s *Store) AddTeamMember(ctx context.Context, accountID, teamID, userID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO team_memberships (team_id, user_id)
		SELECT $2, $3
		 WHERE EXISTS (SELECT 1 FROM teams WHERE id = $2 AND account_id = $1 AND deleted_at IS NULL)
		   AND EXISTS (SELECT 1 FROM memberships WHERE account_id = $1 AND user_id = $3)
		ON CONFLICT (team_id, user_id) DO NOTHING
	`, accountID, teamID, userID)
	if err != nil {
		return err
	}
	// Zero rows means either a genuine no-op (already a member) or a rejected
	// team/user. Re-check membership so a duplicate add reports success while a
	// forged team/user reports ErrNotFound.
	if tag.RowsAffected() == 0 {
		var ok bool
		if err := s.pool.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM team_memberships tm
			 JOIN teams t ON t.id = tm.team_id
			 WHERE tm.team_id = $1 AND tm.user_id = $2 AND t.account_id = $3)
		`, teamID, userID, accountID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return ErrNotFound
		}
	}
	return nil
}

// RemoveTeamMember takes a user off a team, scoped to the account.
func (s *Store) RemoveTeamMember(ctx context.Context, accountID, teamID, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM team_memberships tm
		 USING teams t
		 WHERE tm.team_id = t.id AND t.account_id = $1
		   AND tm.team_id = $2 AND tm.user_id = $3
	`, accountID, teamID, userID)
	return err
}

// TeamIDsForUser returns the IDs of the account's teams the user belongs to.
// This is the input to drill.Scope for a non-privileged member.
func (s *Store) TeamIDsForUser(ctx context.Context, accountID, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT tm.team_id
		  FROM team_memberships tm
		  JOIN teams t ON t.id = tm.team_id
		 WHERE t.account_id = $1 AND t.deleted_at IS NULL AND tm.user_id = $2
	`, accountID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// TeamMemberships returns every (team, user) edge in the account, so the
// management page can bucket members under teams in one query.
func (s *Store) TeamMemberships(ctx context.Context, accountID uuid.UUID) ([]TeamMembershipRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT tm.team_id, tm.user_id
		  FROM team_memberships tm
		  JOIN teams t ON t.id = tm.team_id
		 WHERE t.account_id = $1 AND t.deleted_at IS NULL
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TeamMembershipRow
	for rows.Next() {
		var r TeamMembershipRow
		if err := rows.Scan(&r.TeamID, &r.UserID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetTeam loads one team scoped to the account.
func (s *Store) GetTeam(ctx context.Context, accountID, teamID uuid.UUID) (Team, error) {
	var t Team
	err := s.pool.QueryRow(ctx, `
		SELECT id, account_id, name FROM teams
		 WHERE id = $1 AND account_id = $2 AND deleted_at IS NULL
	`, teamID, accountID).Scan(&t.ID, &t.AccountID, &t.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return Team{}, ErrNotFound
	}
	return t, err
}
