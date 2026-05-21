package webhooks

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// RiverInserter is the subset of *river.Client the dispatcher needs. Keeping
// it an interface lets tests enqueue against a fake.
type RiverInserter interface {
	Insert(ctx context.Context, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

// Dispatcher fans an event out to every enabled endpoint on an account: it
// records a pending delivery row per endpoint and enqueues a River job for
// each. Safe to call from a step worker — failures to enqueue are returned
// but a partial fan-out still persists the rows it managed to create.
type Dispatcher struct {
	store    *Store
	inserter RiverInserter
}

func NewDispatcher(store *Store, inserter RiverInserter) *Dispatcher {
	return &Dispatcher{store: store, inserter: inserter}
}

// Dispatch builds the signed payload once, then creates + enqueues a delivery
// for each enabled endpoint. No endpoints → no-op.
func (d *Dispatcher) Dispatch(ctx context.Context, accountID uuid.UUID, event string, data map[string]any) error {
	endpoints, err := d.store.ListEnabledEndpoints(ctx, accountID)
	if err != nil {
		return fmt.Errorf("list endpoints: %w", err)
	}
	if len(endpoints) == 0 {
		return nil
	}

	payload, err := MarshalPayload(event, accountID.String(), data)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	for _, e := range endpoints {
		deliveryID, err := d.store.CreateDelivery(ctx, e.ID, accountID, event, payload)
		if err != nil {
			return fmt.Errorf("create delivery: %w", err)
		}
		if _, err := d.inserter.Insert(ctx, DeliverArgs{DeliveryID: deliveryID}, nil); err != nil {
			return fmt.Errorf("enqueue delivery: %w", err)
		}
	}
	return nil
}

// Enqueue schedules a single delivery job — used by the dashboard "replay"
// action, which creates the delivery row itself.
func (d *Dispatcher) Enqueue(ctx context.Context, deliveryID uuid.UUID) error {
	_, err := d.inserter.Insert(ctx, DeliverArgs{DeliveryID: deliveryID}, nil)
	return err
}
