// Package runner defines the sandbox abstraction that executes drill steps
// in isolation from the application database. Phase 2 ships a local mock that
// uses temp databases on the same Postgres host; later phases add Fly Machines
// and a customer-VPC runner.
package runner

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ErrNotImplemented is returned by stub runners (e.g. the Fly Machines runner
// in Phase 2) so callers can detect "this is wired but not built yet".
var ErrNotImplemented = errors.New("runner: not implemented")

// Sandbox is a handle to an isolated execution environment for a single drill.
// It is created by Provision and torn down by Teardown. Between those calls,
// Restore and Query operate against the same isolated database.
type Sandbox struct {
	DrillID uuid.UUID
	// DSN is a libpq URL for the sandbox database. It is *not* shared with the
	// app pool; the caller is expected to dial it as needed.
	DSN string
	// Name is the bare database name (handy for logging + pg_restore -d).
	Name string
}

// Runner is the sandbox abstraction. Implementations must be safe for
// concurrent use across drills. Each method takes a drill-scoped context so
// timeouts and cancellations propagate.
type Runner interface {
	// Provision returns a fresh, empty sandbox dedicated to this drill.
	Provision(ctx context.Context, drillID uuid.UUID) (*Sandbox, error)

	// Fetch retrieves the dump pointed at by sourceURI into a location
	// reachable by Restore. For the local-file runner this is a no-op that
	// just validates the file exists; later runners will copy from R2/S3.
	// Returns the local path Restore should consume.
	Fetch(ctx context.Context, sb *Sandbox, sourceURI string) (localPath string, err error)

	// Restore applies the dump at localPath into the sandbox database.
	//
	// Assertions are *not* part of the Runner contract: the assert step dials
	// Sandbox.DSN directly and runs each configured check via the assertions
	// package, so the runner stays a pure provision/restore/teardown surface.
	Restore(ctx context.Context, sb *Sandbox, localPath string) error

	// Teardown destroys the sandbox. Must be safe to call on a partially
	// provisioned sandbox.
	Teardown(ctx context.Context, sb *Sandbox) error
}
