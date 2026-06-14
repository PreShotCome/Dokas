package heartbeat

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/preshotcome/dokaz/internal/audit"
)

type fakeDispatcher struct {
	mu     sync.Mutex
	events []string
}

func (f *fakeDispatcher) Dispatch(_ context.Context, _ uuid.UUID, event string, _ map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event)
	return nil
}

type fakeAuditor struct {
	mu      sync.Mutex
	actions []string
}

func (f *fakeAuditor) Record(_ context.Context, e audit.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, e.Action)
	return nil
}

func TestSweeperWorkerFiresDownEdge(t *testing.T) {
	s, pool := testStore(t)
	ctx := context.Background()
	acct, user := seedAccount(t, ctx, pool)
	hb := newMonitor(t, ctx, s, acct, user, 60, 0)

	if _, err := pool.Exec(ctx, `
		UPDATE heartbeats SET expected_by = now() - interval '1 hour' WHERE id = $1
	`, hb.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	disp := &fakeDispatcher{}
	aud := &fakeAuditor{}
	w := &SweeperWorker{Store: s, Dispatch: disp, Audit: aud}

	if err := w.Work(ctx, &river.Job[SweeperArgs]{}); err != nil {
		t.Fatalf("work: %v", err)
	}

	if len(disp.events) != 1 || disp.events[0] != EventDown {
		t.Fatalf("dispatched events = %v, want one %q", disp.events, EventDown)
	}
	if len(aud.actions) != 1 || aud.actions[0] != EventDown {
		t.Fatalf("audit actions = %v, want one %q", aud.actions, EventDown)
	}

	// A second sweep with no newly-overdue monitors fires nothing.
	if err := w.Work(ctx, &river.Job[SweeperArgs]{}); err != nil {
		t.Fatalf("work 2: %v", err)
	}
	if len(disp.events) != 1 {
		t.Fatalf("second sweep dispatched again: %v", disp.events)
	}
}
