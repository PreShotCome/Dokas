// Package drill holds the drill domain: targets, drills, steps, assertions,
// and the orchestrator that ties them together with River.
package drill

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// StepName enumerates the seven-step workflow. Slice order matters: it
// defines the execution order the orchestrator enqueues.
type StepName string

const (
	StepProvision StepName = "provision"
	StepFetch     StepName = "fetch"
	StepRestore   StepName = "restore"
	StepAssert    StepName = "assert"
	StepReport    StepName = "report"
	StepTeardown  StepName = "teardown"
)

// AllSteps is the canonical execution order.
var AllSteps = []StepName{
	StepProvision, StepFetch, StepRestore, StepAssert, StepReport, StepTeardown,
}

type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusSkipped   Status = "skipped"
)

func (s Status) Terminal() bool {
	return s == StatusSucceeded || s == StatusFailed || s == StatusSkipped
}

type Target struct {
	ID                uuid.UUID
	UserID            uuid.UUID
	Name              string
	SourceKind        string
	SourceURI         string
	AssertionTable    string
	AssertionMinRows  int
	CreatedAt         time.Time
}

type Drill struct {
	ID            uuid.UUID
	TargetID      uuid.UUID
	UserID        uuid.UUID
	Status        Status
	StartedAt     *time.Time
	CompletedAt   *time.Time
	Error         *string
	EvidencePath  *string
	SandboxDB     *string
	CreatedAt     time.Time
}

type Step struct {
	ID             uuid.UUID
	DrillID        uuid.UUID
	Name           StepName
	Status         Status
	StartedAt      *time.Time
	CompletedAt    *time.Time
	Error          *string
	IdempotencyKey string
	Ordinal        int
}

type AssertionResult struct {
	ID       uuid.UUID
	DrillID  uuid.UUID
	Kind     string
	Expected []byte
	Actual   []byte
	Passed   bool
	At       time.Time
}

// Store is a thin data-access layer over pgx. Handlers and step workers go
// through it instead of writing SQL inline so the schema is referenced in
// exactly one place per query.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Pool exposes the underlying pool for callers that need to run their own
// queries inside the same transaction lifecycle (e.g. the orchestrator).
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

var ErrNotFound = errors.New("drill: not found")

// --- targets ---

func (s *Store) CreateTarget(ctx context.Context, t Target) (Target, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO database_targets
		    (user_id, name, source_kind, source_uri, assertion_table, assertion_min_rows)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at
	`, t.UserID, t.Name, t.SourceKind, t.SourceURI, t.AssertionTable, t.AssertionMinRows).
		Scan(&t.ID, &t.CreatedAt)
	return t, err
}

func (s *Store) ListTargets(ctx context.Context, userID uuid.UUID) ([]Target, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, name, source_kind, source_uri,
		       assertion_table, assertion_min_rows, created_at
		  FROM database_targets
		 WHERE user_id = $1 AND deleted_at IS NULL
		 ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Target
	for rows.Next() {
		var t Target
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.SourceKind, &t.SourceURI,
			&t.AssertionTable, &t.AssertionMinRows, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) GetTarget(ctx context.Context, userID, targetID uuid.UUID) (Target, error) {
	var t Target
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, name, source_kind, source_uri,
		       assertion_table, assertion_min_rows, created_at
		  FROM database_targets
		 WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
	`, targetID, userID).Scan(&t.ID, &t.UserID, &t.Name, &t.SourceKind, &t.SourceURI,
		&t.AssertionTable, &t.AssertionMinRows, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Target{}, ErrNotFound
	}
	return t, err
}

// --- drills ---

func (s *Store) CreateDrill(ctx context.Context, d Drill) (Drill, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO drills (target_id, user_id, status)
		VALUES ($1, $2, 'pending')
		RETURNING id, status, created_at
	`, d.TargetID, d.UserID).Scan(&d.ID, &d.Status, &d.CreatedAt)
	return d, err
}

func (s *Store) GetDrill(ctx context.Context, userID, drillID uuid.UUID) (Drill, error) {
	var d Drill
	err := s.pool.QueryRow(ctx, `
		SELECT id, target_id, user_id, status, started_at, completed_at,
		       error, evidence_path, sandbox_db, created_at
		  FROM drills
		 WHERE id = $1 AND user_id = $2
	`, drillID, userID).Scan(&d.ID, &d.TargetID, &d.UserID, &d.Status,
		&d.StartedAt, &d.CompletedAt, &d.Error, &d.EvidencePath, &d.SandboxDB, &d.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Drill{}, ErrNotFound
	}
	return d, err
}

// GetDrillByID is the no-auth internal lookup, used by step workers.
func (s *Store) GetDrillByID(ctx context.Context, drillID uuid.UUID) (Drill, error) {
	var d Drill
	err := s.pool.QueryRow(ctx, `
		SELECT id, target_id, user_id, status, started_at, completed_at,
		       error, evidence_path, sandbox_db, created_at
		  FROM drills
		 WHERE id = $1
	`, drillID).Scan(&d.ID, &d.TargetID, &d.UserID, &d.Status,
		&d.StartedAt, &d.CompletedAt, &d.Error, &d.EvidencePath, &d.SandboxDB, &d.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Drill{}, ErrNotFound
	}
	return d, err
}

func (s *Store) GetTargetByID(ctx context.Context, targetID uuid.UUID) (Target, error) {
	var t Target
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, name, source_kind, source_uri,
		       assertion_table, assertion_min_rows, created_at
		  FROM database_targets
		 WHERE id = $1
	`, targetID).Scan(&t.ID, &t.UserID, &t.Name, &t.SourceKind, &t.SourceURI,
		&t.AssertionTable, &t.AssertionMinRows, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Target{}, ErrNotFound
	}
	return t, err
}

func (s *Store) ListDrills(ctx context.Context, userID uuid.UUID, limit int) ([]Drill, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, target_id, user_id, status, started_at, completed_at,
		       error, evidence_path, sandbox_db, created_at
		  FROM drills
		 WHERE user_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Drill
	for rows.Next() {
		var d Drill
		if err := rows.Scan(&d.ID, &d.TargetID, &d.UserID, &d.Status,
			&d.StartedAt, &d.CompletedAt, &d.Error, &d.EvidencePath, &d.SandboxDB, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) MarkDrillRunning(ctx context.Context, drillID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE drills
		   SET status = 'running', started_at = COALESCE(started_at, now())
		 WHERE id = $1 AND status IN ('pending','running')
	`, drillID)
	return err
}

func (s *Store) MarkDrillSucceeded(ctx context.Context, drillID uuid.UUID, evidencePath string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE drills
		   SET status = 'succeeded',
		       completed_at = now(),
		       evidence_path = $2,
		       error = NULL
		 WHERE id = $1
	`, drillID, evidencePath)
	return err
}

func (s *Store) MarkDrillFailed(ctx context.Context, drillID uuid.UUID, reason string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE drills
		   SET status = 'failed',
		       completed_at = COALESCE(completed_at, now()),
		       error = $2
		 WHERE id = $1
	`, drillID, reason)
	return err
}

func (s *Store) SetSandboxDB(ctx context.Context, drillID uuid.UUID, name string) error {
	_, err := s.pool.Exec(ctx, `UPDATE drills SET sandbox_db = $2 WHERE id = $1`, drillID, name)
	return err
}

// MarkEvidence stores the evidence path. Used by the report step so the file
// is downloadable as soon as it's written, even before teardown completes.
func (s *Store) MarkEvidence(ctx context.Context, drillID uuid.UUID, path string) error {
	_, err := s.pool.Exec(ctx, `UPDATE drills SET evidence_path = $2 WHERE id = $1`, drillID, path)
	return err
}

// --- steps ---

func (s *Store) CreateStepIfMissing(ctx context.Context, drillID uuid.UUID, name StepName, ordinal int, idemKey string) (Step, error) {
	var step Step
	err := s.pool.QueryRow(ctx, `
		INSERT INTO drill_steps (drill_id, name, ordinal, idempotency_key)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (drill_id, name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id, drill_id, name, status, started_at, completed_at, error, idempotency_key, ordinal
	`, drillID, name, ordinal, idemKey).Scan(
		&step.ID, &step.DrillID, &step.Name, &step.Status,
		&step.StartedAt, &step.CompletedAt, &step.Error, &step.IdempotencyKey, &step.Ordinal,
	)
	return step, err
}

func (s *Store) GetStep(ctx context.Context, drillID uuid.UUID, name StepName) (Step, error) {
	var step Step
	err := s.pool.QueryRow(ctx, `
		SELECT id, drill_id, name, status, started_at, completed_at, error, idempotency_key, ordinal
		  FROM drill_steps WHERE drill_id = $1 AND name = $2
	`, drillID, name).Scan(
		&step.ID, &step.DrillID, &step.Name, &step.Status,
		&step.StartedAt, &step.CompletedAt, &step.Error, &step.IdempotencyKey, &step.Ordinal,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Step{}, ErrNotFound
	}
	return step, err
}

func (s *Store) ListSteps(ctx context.Context, drillID uuid.UUID) ([]Step, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, drill_id, name, status, started_at, completed_at, error, idempotency_key, ordinal
		  FROM drill_steps WHERE drill_id = $1 ORDER BY ordinal ASC
	`, drillID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Step
	for rows.Next() {
		var step Step
		if err := rows.Scan(&step.ID, &step.DrillID, &step.Name, &step.Status,
			&step.StartedAt, &step.CompletedAt, &step.Error, &step.IdempotencyKey, &step.Ordinal); err != nil {
			return nil, err
		}
		out = append(out, step)
	}
	return out, rows.Err()
}

func (s *Store) MarkStepRunning(ctx context.Context, drillID uuid.UUID, name StepName) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE drill_steps
		   SET status = 'running', started_at = COALESCE(started_at, now())
		 WHERE drill_id = $1 AND name = $2 AND status IN ('pending','running','failed')
	`, drillID, name)
	return err
}

func (s *Store) MarkStepSucceeded(ctx context.Context, drillID uuid.UUID, name StepName) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE drill_steps
		   SET status = 'succeeded', completed_at = now(), error = NULL
		 WHERE drill_id = $1 AND name = $2
	`, drillID, name)
	return err
}

func (s *Store) MarkStepFailed(ctx context.Context, drillID uuid.UUID, name StepName, reason string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE drill_steps
		   SET status = 'failed', completed_at = now(), error = $3
		 WHERE drill_id = $1 AND name = $2
	`, drillID, name, reason)
	return err
}

func (s *Store) MarkStepSkipped(ctx context.Context, drillID uuid.UUID, name StepName) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE drill_steps
		   SET status = 'skipped', completed_at = now()
		 WHERE drill_id = $1 AND name = $2 AND status = 'pending'
	`, drillID, name)
	return err
}

// --- assertion results ---

func (s *Store) RecordAssertion(ctx context.Context, ar AssertionResult) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO assertion_results (drill_id, kind, expected, actual, passed)
		VALUES ($1, $2, $3::jsonb, $4::jsonb, $5)
	`, ar.DrillID, ar.Kind, string(ar.Expected), string(ar.Actual), ar.Passed)
	return err
}

func (s *Store) ListAssertions(ctx context.Context, drillID uuid.UUID) ([]AssertionResult, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, drill_id, kind, expected, actual, passed, at
		  FROM assertion_results WHERE drill_id = $1 ORDER BY at ASC
	`, drillID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AssertionResult
	for rows.Next() {
		var ar AssertionResult
		if err := rows.Scan(&ar.ID, &ar.DrillID, &ar.Kind, &ar.Expected, &ar.Actual, &ar.Passed, &ar.At); err != nil {
			return nil, err
		}
		out = append(out, ar)
	}
	return out, rows.Err()
}

// --- idempotency ---

// CreateDrillIdempotent creates a new drill iff the (user, key) tuple has not
// already been claimed. If it has, returns the previously created drill_id and
// reused=true. Atomic via the unique index on idempotency_keys.
func (s *Store) CreateDrillIdempotent(ctx context.Context, userID, targetID uuid.UUID, key string) (drillID uuid.UUID, reused bool, err error) {
	if key == "" {
		return uuid.Nil, false, errors.New("empty idempotency key")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	const scope = "drill_create"

	var existing string
	err = tx.QueryRow(ctx, `
		SELECT target_id FROM idempotency_keys
		 WHERE user_id = $1 AND key = $2 AND scope = $3
	`, userID, key, scope).Scan(&existing)
	switch {
	case err == nil:
		id, parseErr := uuid.Parse(existing)
		if parseErr != nil {
			return uuid.Nil, false, parseErr
		}
		return id, true, nil
	case errors.Is(err, pgx.ErrNoRows):
		// fall through and create
	default:
		return uuid.Nil, false, err
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO drills (target_id, user_id, status)
		VALUES ($1, $2, 'pending')
		RETURNING id
	`, targetID, userID).Scan(&drillID)
	if err != nil {
		return uuid.Nil, false, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO idempotency_keys (user_id, key, scope, target_id)
		VALUES ($1, $2, $3, $4)
	`, userID, key, scope, drillID.String()); err != nil {
		return uuid.Nil, false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, false, err
	}
	return drillID, false, nil
}
