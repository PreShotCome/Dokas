package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/preshotcome/anything/internal/fly"
)

// flyCleanupTimeout is how long we allow a machine-destroy call to run on
// the cleanup path, regardless of how the original job context ended.
const flyCleanupTimeout = 30 * time.Second

// FlyMachineRunner runs each drill in a dedicated, ephemeral Fly Machine — a
// per-drill Postgres VM, isolated from the app and from other drills.
// Provision creates the machine; Restore loads the dump into it (the app
// runs pg_restore against the machine's private DSN); Teardown destroys it.
// Fetch and Restore reuse the local-runner logic — only the sandbox
// lifecycle differs.
//
// Reaching a machine's Postgres requires the app to share Fly's private
// network with it (6PN); see docs/runbooks/fly.md. This runner is
// build-verified — it has not been exercised against the live Fly API.
type FlyMachineRunner struct {
	fly      *fly.Client
	app      string
	image    string // a Postgres container image
	region   string
	dbPass   string // POSTGRES_PASSWORD baked into every sandbox machine
	memoryMB int
}

// NewFlyMachineRunner builds the runner. image must be a Postgres container
// image — pin it by digest (postgres:16-alpine@sha256:…) in production so
// every drill's sandbox is bit-for-bit reproducible, which is what an
// auditor expects when they read the evidence chain. The floating
// "postgres:16-alpine" default is dev-only. dbPass is the password every
// sandbox Postgres is created with — fixed, so Rehydrate can rebuild the
// DSN.
func NewFlyMachineRunner(client *fly.Client, app, image, region, dbPass string) *FlyMachineRunner {
	if image == "" {
		image = "postgres:16-alpine"
	}
	return &FlyMachineRunner{
		fly: client, app: app, image: image, region: region,
		dbPass: dbPass, memoryMB: 1024,
	}
}

// dsn builds the libpq URL for a sandbox machine. Fly's private DNS resolves
// <machine-id>.vm.<app>.internal over the 6PN network.
func (r *FlyMachineRunner) dsn(machineID string) string {
	return fmt.Sprintf("postgres://postgres:%s@%s.vm.%s.internal:5432/postgres?sslmode=disable",
		r.dbPass, machineID, r.app)
}

func (r *FlyMachineRunner) Provision(ctx context.Context, drillID uuid.UUID) (*Sandbox, error) {
	m, err := r.fly.CreateMachine(ctx, fly.CreateInput{
		Image:  r.image,
		Region: r.region,
		Env: map[string]string{
			"POSTGRES_PASSWORD": r.dbPass,
			"POSTGRES_DB":       "postgres",
		},
		MemoryMB: r.memoryMB,
		CPUs:     1,
	})
	if err != nil {
		return nil, fmt.Errorf("fly: provision machine: %w", err)
	}
	if err := r.fly.WaitStarted(ctx, m.ID); err != nil {
		// Use a fresh context for cleanup: if the job timed out or was
		// cancelled, the inherited ctx is already done and Destroy would
		// return instantly, leaking the machine. context.WithoutCancel
		// (Go 1.21+) inherits values but not cancellation.
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), flyCleanupTimeout)
		_ = r.fly.Destroy(cleanupCtx, m.ID) // never orphan a machine
		cancel()
		return nil, fmt.Errorf("fly: machine %s never started: %w", m.ID, err)
	}
	return &Sandbox{DrillID: drillID, Name: m.ID, DSN: r.dsn(m.ID)}, nil
}

func (r *FlyMachineRunner) Fetch(_ context.Context, _ *Sandbox, sourceURI string) (string, string, error) {
	return fetchDump(sourceURI)
}

func (r *FlyMachineRunner) Restore(ctx context.Context, sb *Sandbox, localPath string) ([]byte, error) {
	return restoreDump(ctx, sb.DSN, localPath)
}

// Rehydrate rebuilds a Sandbox from a drill ID and the persisted machine ID.
func (r *FlyMachineRunner) Rehydrate(drillID uuid.UUID, machineID string) (*Sandbox, error) {
	return &Sandbox{DrillID: drillID, Name: machineID, DSN: r.dsn(machineID)}, nil
}

// Teardown destroys the drill's machine. Safe on a nil/zero sandbox.
func (r *FlyMachineRunner) Teardown(ctx context.Context, sb *Sandbox) error {
	if sb == nil || sb.Name == "" {
		return nil
	}
	return r.fly.Destroy(ctx, sb.Name)
}

// Compile-time interface guard.
var _ Runner = (*FlyMachineRunner)(nil)
