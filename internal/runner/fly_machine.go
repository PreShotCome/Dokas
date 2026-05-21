package runner

import (
	"context"

	"github.com/google/uuid"
)

// FlyMachineRunner is the production sandbox driver. It will provision a
// dedicated Fly Machine per drill, attach a volume, fetch the dump from
// object storage, run pg_restore against an in-machine Postgres, run
// assertions, and destroy the machine on teardown.
//
// Phase 2 ships the type and interface compliance only. Real wiring is
// deferred to a later phase; every method returns ErrNotImplemented so
// callers can detect "this is the stub" and fall back to the local runner.
type FlyMachineRunner struct {
	// AppName, APIToken, Region, VolumeSizeGB, MachineCPUKind, MachineCPU,
	// MachineMemoryMB — populated when the real runner lands.
}

func NewFlyMachineRunner() *FlyMachineRunner { return &FlyMachineRunner{} }

func (f *FlyMachineRunner) Provision(_ context.Context, _ uuid.UUID) (*Sandbox, error) {
	return nil, ErrNotImplemented
}

func (f *FlyMachineRunner) Fetch(_ context.Context, _ *Sandbox, _ string) (string, error) {
	return "", ErrNotImplemented
}

func (f *FlyMachineRunner) Restore(_ context.Context, _ *Sandbox, _ string) error {
	return ErrNotImplemented
}

func (f *FlyMachineRunner) Teardown(_ context.Context, _ *Sandbox) error {
	return ErrNotImplemented
}

// Compile-time interface guard.
var _ Runner = (*FlyMachineRunner)(nil)
