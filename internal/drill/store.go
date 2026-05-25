// Package drill holds the drill domain: targets, drills, steps, assertions,
// and the orchestrator that ties them together with River.
package drill

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// hexEncode is a local alias so the call site in SetStepOutput reads
// concisely; hex.EncodeToString is the only consumer.
func hexEncode(b []byte) string { return hex.EncodeToString(b) }

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
	ID              uuid.UUID
	AccountID       uuid.UUID
	CreatedByUserID uuid.UUID
	Name            string
	SourceKind      string
	SourceURI       string
	CreatedAt       time.Time
	// DrillCadence is "off", "weekly", "daily", or "hourly". NextDrillAt is
	// when the scheduler should next enqueue a drill (nil when cadence is off).
	DrillCadence string
	NextDrillAt  *time.Time
}

// Assertion is one configured check on a target. config holds the raw JSONB
// blob; the assertions package decodes it per kind. A target carries zero or
// more; the assert step runs every one against the restored sandbox.
type Assertion struct {
	ID        uuid.UUID
	TargetID  uuid.UUID
	Kind      string
	Config    []byte
	CreatedAt time.Time
}

type Drill struct {
	ID              uuid.UUID
	TargetID        uuid.UUID
	AccountID       uuid.UUID
	CreatedByUserID uuid.UUID
	Status          Status
	StartedAt       *time.Time
	CompletedAt     *time.Time
	Error           *string
	EvidencePath    *string
	SandboxDB       *string
	// SourceHash is the hex SHA-256 of the dump bytes fetched for this
	// drill. The input anchor of the evidence chain — a customer who
	// re-hashes the dump they hold can prove it's the exact file we
	// drilled. NULL for drills that ran before this column existed.
	SourceHash *string
	CreatedAt  time.Time
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
	// OutputSnippet / OutputSHA256 / OutputTruncated capture stdout+stderr
	// of any subprocess the step ran (today: only restore). The snippet is
	// what lives in the signed PDF; the hash covers the *full* output so a
	// holder of the original dump can re-run the same tool and confirm
	// the snippet is a true prefix.
	OutputSnippet   *string
	OutputSHA256    *string
	OutputTruncated *bool
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
		    (account_id, created_by_user_id, name, source_kind, source_uri)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at
	`, t.AccountID, t.CreatedByUserID, t.Name, t.SourceKind, t.SourceURI).
		Scan(&t.ID, &t.CreatedAt)
	return t, err
}

func (s *Store) ListTargets(ctx context.Context, accountID uuid.UUID) ([]Target, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, account_id, created_by_user_id, name, source_kind, source_uri, created_at,
		       drill_cadence, next_drill_at
		  FROM database_targets
		 WHERE account_id = $1 AND deleted_at IS NULL
		 ORDER BY created_at DESC
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Target
	for rows.Next() {
		var t Target
		if err := rows.Scan(&t.ID, &t.AccountID, &t.CreatedByUserID, &t.Name, &t.SourceKind, &t.SourceURI,
			&t.CreatedAt, &t.DrillCadence, &t.NextDrillAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListTargetsPage is the keyset-paginated target list for the /v1 API.
// Pass a nil cursor for the first page; rows order (created_at, id) DESC.
func (s *Store) ListTargetsPage(ctx context.Context, accountID uuid.UUID, afterAt *time.Time, afterID *uuid.UUID, limit int) ([]Target, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, account_id, created_by_user_id, name, source_kind, source_uri, created_at
		  FROM database_targets
		 WHERE account_id = $1 AND deleted_at IS NULL
		   AND ($2::timestamptz IS NULL OR (created_at, id) < ($2, $3))
		 ORDER BY created_at DESC, id DESC
		 LIMIT $4
	`, accountID, afterAt, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Target
	for rows.Next() {
		var t Target
		if err := rows.Scan(&t.ID, &t.AccountID, &t.CreatedByUserID, &t.Name, &t.SourceKind, &t.SourceURI,
			&t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListDrillsPage is the keyset-paginated drill list for the /v1 API.
func (s *Store) ListDrillsPage(ctx context.Context, accountID uuid.UUID, afterAt *time.Time, afterID *uuid.UUID, limit int) ([]Drill, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, target_id, account_id, created_by_user_id, status, started_at, completed_at,
		       error, evidence_path, sandbox_db, source_hash, created_at
		  FROM drills
		 WHERE account_id = $1
		   AND ($2::timestamptz IS NULL OR (created_at, id) < ($2, $3))
		 ORDER BY created_at DESC, id DESC
		 LIMIT $4
	`, accountID, afterAt, afterID, limit)
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

func (s *Store) GetTarget(ctx context.Context, accountID, targetID uuid.UUID) (Target, error) {
	var t Target
	err := s.pool.QueryRow(ctx, `
		SELECT id, account_id, created_by_user_id, name, source_kind, source_uri, created_at,
		       drill_cadence, next_drill_at
		  FROM database_targets
		 WHERE id = $1 AND account_id = $2 AND deleted_at IS NULL
	`, targetID, accountID).Scan(&t.ID, &t.AccountID, &t.CreatedByUserID, &t.Name, &t.SourceKind, &t.SourceURI,
		&t.CreatedAt, &t.DrillCadence, &t.NextDrillAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Target{}, ErrNotFound
	}
	return t, err
}

// SetTargetSchedule updates a target's drill cadence and its next run time.
// Pass cadence "off" with a nil nextAt to disable scheduling.
func (s *Store) SetTargetSchedule(ctx context.Context, accountID, targetID uuid.UUID, cadence string, nextAt *time.Time) error {
	ct, err := s.pool.Exec(ctx, `
		UPDATE database_targets
		   SET drill_cadence = $3, next_drill_at = $4
		 WHERE id = $1 AND account_id = $2 AND deleted_at IS NULL
	`, targetID, accountID, cadence, nextAt)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DueTargets returns every scheduled target whose next drill is due. Used by
// the scheduler worker.
//
// The query takes a row-level lock with SKIP LOCKED so a second concurrent
// scheduler (a crash-loop restart, or a deliberately-scaled pool) processes
// a disjoint slice of the queue instead of racing with the first one. The
// lock is released when the caller's transaction commits — here we run on
// the pool's auto-transaction, so the lock lives only for the query's
// duration; that's enough to prevent two workers from picking up the same
// row in the same instant.
func (s *Store) DueTargets(ctx context.Context) ([]Target, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.account_id, t.created_by_user_id, t.name, t.source_kind, t.source_uri,
		       t.created_at, t.drill_cadence, t.next_drill_at
		  FROM database_targets t
		  JOIN accounts a ON a.id = t.account_id
		 WHERE t.deleted_at IS NULL
		   AND a.deleted_at IS NULL
		   AND t.drill_cadence <> 'off'
		   AND t.next_drill_at IS NOT NULL
		   AND t.next_drill_at <= now()
		   -- A lapsed trial is read-only: the scheduler stops drilling it.
		   AND NOT (a.plan = 'trial' AND a.trial_ends_at IS NOT NULL AND a.trial_ends_at < now())
		 FOR UPDATE OF t SKIP LOCKED
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Target
	for rows.Next() {
		var t Target
		if err := rows.Scan(&t.ID, &t.AccountID, &t.CreatedByUserID, &t.Name, &t.SourceKind, &t.SourceURI,
			&t.CreatedAt, &t.DrillCadence, &t.NextDrillAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// AdvanceTargetSchedule moves a target's next drill time forward after the
// scheduler has enqueued its drill.
func (s *Store) AdvanceTargetSchedule(ctx context.Context, targetID uuid.UUID, nextAt time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE database_targets SET next_drill_at = $2 WHERE id = $1`, targetID, nextAt)
	return err
}

// --- drills ---

func (s *Store) GetDrill(ctx context.Context, accountID, drillID uuid.UUID) (Drill, error) {
	var d Drill
	err := s.pool.QueryRow(ctx, `
		SELECT id, target_id, account_id, created_by_user_id, status, started_at, completed_at,
		       error, evidence_path, sandbox_db, source_hash, created_at
		  FROM drills
		 WHERE id = $1 AND account_id = $2
	`, drillID, accountID).Scan(&d.ID, &d.TargetID, &d.AccountID, &d.CreatedByUserID, &d.Status,
		&d.StartedAt, &d.CompletedAt, &d.Error, &d.EvidencePath, &d.SandboxDB, &d.SourceHash, &d.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Drill{}, ErrNotFound
	}
	return d, err
}

// GetDrillByID is the no-auth internal lookup, used by step workers.
func (s *Store) GetDrillByID(ctx context.Context, drillID uuid.UUID) (Drill, error) {
	var d Drill
	err := s.pool.QueryRow(ctx, `
		SELECT id, target_id, account_id, created_by_user_id, status, started_at, completed_at,
		       error, evidence_path, sandbox_db, source_hash, created_at
		  FROM drills
		 WHERE id = $1
	`, drillID).Scan(&d.ID, &d.TargetID, &d.AccountID, &d.CreatedByUserID, &d.Status,
		&d.StartedAt, &d.CompletedAt, &d.Error, &d.EvidencePath, &d.SandboxDB, &d.SourceHash, &d.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Drill{}, ErrNotFound
	}
	return d, err
}

func (s *Store) GetTargetByID(ctx context.Context, targetID uuid.UUID) (Target, error) {
	var t Target
	err := s.pool.QueryRow(ctx, `
		SELECT id, account_id, created_by_user_id, name, source_kind, source_uri, created_at
		  FROM database_targets
		 WHERE id = $1
	`, targetID).Scan(&t.ID, &t.AccountID, &t.CreatedByUserID, &t.Name, &t.SourceKind, &t.SourceURI, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Target{}, ErrNotFound
	}
	return t, err
}

// --- target assertions ---

// ListAssertions returns every assertion configured on a target, oldest
// first. No account scoping — callers (handlers) authorise the target first;
// step workers run cross-account by design.
func (s *Store) ListTargetAssertions(ctx context.Context, targetID uuid.UUID) ([]Assertion, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, target_id, kind, config, created_at
		  FROM assertions WHERE target_id = $1 ORDER BY created_at ASC, id ASC
	`, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Assertion
	for rows.Next() {
		var a Assertion
		if err := rows.Scan(&a.ID, &a.TargetID, &a.Kind, &a.Config, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListAssertionsForTargets batch-loads assertions for many targets at once,
// keyed by target_id — used by the /v1 list endpoint to avoid N+1 queries.
func (s *Store) ListAssertionsForTargets(ctx context.Context, targetIDs []uuid.UUID) (map[uuid.UUID][]Assertion, error) {
	out := make(map[uuid.UUID][]Assertion)
	if len(targetIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, target_id, kind, config, created_at
		  FROM assertions WHERE target_id = ANY($1) ORDER BY created_at ASC, id ASC
	`, targetIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a Assertion
		if err := rows.Scan(&a.ID, &a.TargetID, &a.Kind, &a.Config, &a.CreatedAt); err != nil {
			return nil, err
		}
		out[a.TargetID] = append(out[a.TargetID], a)
	}
	return out, rows.Err()
}

// CreateAssertion attaches an assertion to a target. config must be valid
// JSON; the caller is expected to have validated kind + config first.
func (s *Store) CreateAssertion(ctx context.Context, targetID uuid.UUID, kind string, config []byte) (Assertion, error) {
	a := Assertion{TargetID: targetID, Kind: kind, Config: config}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO assertions (target_id, kind, config)
		VALUES ($1, $2, $3::jsonb)
		RETURNING id, created_at
	`, targetID, kind, string(config)).Scan(&a.ID, &a.CreatedAt)
	return a, err
}

// DeleteAssertion removes an assertion, scoped to the account that owns its
// target so one account cannot delete another's assertions.
func (s *Store) DeleteAssertion(ctx context.Context, accountID, assertionID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM assertions a
		 USING database_targets t
		 WHERE a.id = $1 AND a.target_id = t.id AND t.account_id = $2
	`, assertionID, accountID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListDrillsByCreator returns drills a given user kicked off, newest first.
// Cross-account — for the staff admin panel only.
func (s *Store) ListDrillsByCreator(ctx context.Context, userID uuid.UUID, limit int) ([]Drill, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, target_id, account_id, created_by_user_id, status, started_at, completed_at,
		       error, evidence_path, sandbox_db, source_hash, created_at
		  FROM drills
		 WHERE created_by_user_id = $1
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
		if err := rows.Scan(&d.ID, &d.TargetID, &d.AccountID, &d.CreatedByUserID, &d.Status,
			&d.StartedAt, &d.CompletedAt, &d.Error, &d.EvidencePath, &d.SandboxDB, &d.SourceHash, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) ListDrills(ctx context.Context, accountID uuid.UUID, limit int) ([]Drill, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, target_id, account_id, created_by_user_id, status, started_at, completed_at,
		       error, evidence_path, sandbox_db, source_hash, created_at
		  FROM drills
		 WHERE account_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2
	`, accountID, limit)
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

// SetSourceHash records the SHA-256 of the dump fetched for the drill.
// Called from the fetch step. Idempotent — a re-fetch (e.g. River retry
// after a Postgres blip) records the same value.
func (s *Store) SetSourceHash(ctx context.Context, drillID uuid.UUID, hash string) error {
	_, err := s.pool.Exec(ctx, `UPDATE drills SET source_hash = $2 WHERE id = $1`, drillID, hash)
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
		RETURNING id, drill_id, name, status, started_at, completed_at, error, idempotency_key, ordinal,
		       output_snippet, output_sha256, output_truncated
	`, drillID, name, ordinal, idemKey).Scan(
		&step.ID, &step.DrillID, &step.Name, &step.Status,
		&step.StartedAt, &step.CompletedAt, &step.Error, &step.IdempotencyKey, &step.Ordinal,
			&step.OutputSnippet, &step.OutputSHA256, &step.OutputTruncated,
	)
	return step, err
}

func (s *Store) GetStep(ctx context.Context, drillID uuid.UUID, name StepName) (Step, error) {
	var step Step
	err := s.pool.QueryRow(ctx, `
		SELECT id, drill_id, name, status, started_at, completed_at, error, idempotency_key, ordinal,
		       output_snippet, output_sha256, output_truncated
		  FROM drill_steps WHERE drill_id = $1 AND name = $2
	`, drillID, name).Scan(
		&step.ID, &step.DrillID, &step.Name, &step.Status,
		&step.StartedAt, &step.CompletedAt, &step.Error, &step.IdempotencyKey, &step.Ordinal,
			&step.OutputSnippet, &step.OutputSHA256, &step.OutputTruncated,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Step{}, ErrNotFound
	}
	return step, err
}

func (s *Store) ListSteps(ctx context.Context, drillID uuid.UUID) ([]Step, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, drill_id, name, status, started_at, completed_at, error, idempotency_key, ordinal,
		       output_snippet, output_sha256, output_truncated
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

// stepOutputCap is how many bytes of stdout+stderr we persist per step
// (16 KiB). Enough to fit a typical pg_restore log; small enough that a
// failed-drill blast cannot wedge the database.
const stepOutputCap = 16 * 1024

// SetStepOutput records the subprocess output a step produced. Truncates
// the snippet at 16 KiB and stores the full-output SHA-256 alongside so
// the snippet (and the corresponding PDF row) is tamper-evident even
// when shorter than the real output.
func (s *Store) SetStepOutput(ctx context.Context, drillID uuid.UUID, name StepName, output []byte) error {
	if len(output) == 0 {
		return nil
	}
	sum := sha256.Sum256(output)
	hex := hexEncode(sum[:])
	snippet := output
	truncated := false
	if len(snippet) > stepOutputCap {
		snippet = snippet[:stepOutputCap]
		truncated = true
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE drill_steps
		   SET output_snippet = $3, output_sha256 = $4, output_truncated = $5
		 WHERE drill_id = $1 AND name = $2
	`, drillID, name, string(snippet), hex, truncated)
	return err
}

// MarkStepSucceeded flips a step to succeeded. A 'skipped' step is never
// resurrected — once a failure skips the rest of the pipeline, a stray
// retried job for one of those steps cannot mark it succeeded.
func (s *Store) MarkStepSucceeded(ctx context.Context, drillID uuid.UUID, name StepName) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE drill_steps
		   SET status = 'succeeded', completed_at = now(), error = NULL
		 WHERE drill_id = $1 AND name = $2 AND status <> 'skipped'
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

// ClearAssertionResults removes every recorded result for a drill. The assert
// step calls this before re-running so a job retry doesn't double-record.
func (s *Store) ClearAssertionResults(ctx context.Context, drillID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM assertion_results WHERE drill_id = $1`, drillID)
	return err
}

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

// CreateDrillIdempotent creates a new drill iff the (account, key) tuple has
// not already been claimed. If it has, returns the previously created
// drill_id and reused=true. Atomic via the unique index on idempotency_keys.
// createdByUserID is recorded for audit but doesn't gate uniqueness — two
// members of the same account hitting the same key form-submit dedupe.
func (s *Store) CreateDrillIdempotent(ctx context.Context, accountID, createdByUserID, targetID uuid.UUID, key string) (drillID uuid.UUID, reused bool, err error) {
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
		 WHERE account_id = $1 AND key = $2 AND scope = $3
	`, accountID, key, scope).Scan(&existing)
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
		INSERT INTO drills (target_id, account_id, created_by_user_id, status)
		VALUES ($1, $2, $3, 'pending')
		RETURNING id
	`, targetID, accountID, createdByUserID).Scan(&drillID)
	if err != nil {
		return uuid.Nil, false, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO idempotency_keys (account_id, user_id, key, scope, target_id)
		VALUES ($1, $2, $3, $4, $5)
	`, accountID, createdByUserID, key, scope, drillID.String()); err != nil {
		return uuid.Nil, false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, false, err
	}
	return drillID, false, nil
}

// MonthlyStat aggregates an account's drills for one calendar month.
type MonthlyStat struct {
	Month      time.Time
	Total      int
	Succeeded  int
	Failed     int
	AvgSeconds float64
}

// MonthlyStats returns per-month drill aggregates for an account, oldest
// month first, covering drills created at or after `since`.
func (s *Store) MonthlyStats(ctx context.Context, accountID uuid.UUID, since time.Time) ([]MonthlyStat, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT date_trunc('month', created_at) AS month,
		       count(*) AS total,
		       count(*) FILTER (WHERE status = 'succeeded') AS succeeded,
		       count(*) FILTER (WHERE status = 'failed') AS failed,
		       COALESCE(avg(extract(epoch FROM (completed_at - started_at)))
		                FILTER (WHERE completed_at IS NOT NULL AND started_at IS NOT NULL), 0)
		  FROM drills
		 WHERE account_id = $1 AND created_at >= $2
		 GROUP BY 1 ORDER BY 1
	`, accountID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MonthlyStat
	for rows.Next() {
		var m MonthlyStat
		if err := rows.Scan(&m.Month, &m.Total, &m.Succeeded, &m.Failed, &m.AvgSeconds); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// DatabaseStat aggregates an account's drills for one database target.
type DatabaseStat struct {
	TargetID  uuid.UUID
	Name      string
	Total     int
	Succeeded int
	Failed    int
	LastDrill *time.Time
}

// DatabaseStats returns per-database drill aggregates for an account, over
// drills created at or after `since`. Targets with no drills in the window
// are included with zero counts.
func (s *Store) DatabaseStats(ctx context.Context, accountID uuid.UUID, since time.Time) ([]DatabaseStat, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.name,
		       count(d.id) AS total,
		       count(d.id) FILTER (WHERE d.status = 'succeeded') AS succeeded,
		       count(d.id) FILTER (WHERE d.status = 'failed') AS failed,
		       max(d.created_at) AS last_drill
		  FROM database_targets t
		  LEFT JOIN drills d ON d.target_id = t.id AND d.created_at >= $2
		 WHERE t.account_id = $1 AND t.deleted_at IS NULL
		 GROUP BY t.id, t.name
		 ORDER BY t.name
	`, accountID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DatabaseStat
	for rows.Next() {
		var d DatabaseStat
		if err := rows.Scan(&d.TargetID, &d.Name, &d.Total, &d.Succeeded, &d.Failed, &d.LastDrill); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
