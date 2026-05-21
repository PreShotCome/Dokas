package webhooks

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// seedAccount inserts a bare user + account so webhook FKs resolve.
func seedAccount(t *testing.T, ctx context.Context, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	userID := uuid.New()
	accountID := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash) VALUES ($1, $2, 'x')
	`, userID, "wh-test+"+userID.String()+"@example.com"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, name, slug) VALUES ($1, $2, $3)
	`, accountID, "wh-test", "wh-"+accountID.String()[:8]); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE id = $1`, accountID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})
	return accountID
}

func TestWebhookDeliverySuccess(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()

	store := NewStore(pool)
	accountID := seedAccount(t, ctx, pool)

	// Capture server: records body + signature header, returns 200.
	var (
		mu       sync.Mutex
		gotBody  []byte
		gotSig   string
		gotEvent string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotBody = b
		gotSig = r.Header.Get(SignatureHeader)
		gotEvent = r.Header.Get("X-RestoreDrill-Event")
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	endpoint, err := store.CreateEndpoint(ctx, accountID, srv.URL)
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}

	payload, err := MarshalPayload("drill.completed", accountID.String(), map[string]any{"drill_id": "abc"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	deliveryID, err := store.CreateDelivery(ctx, endpoint.ID, accountID, "drill.completed", payload)
	if err != nil {
		t.Fatalf("create delivery: %v", err)
	}

	worker := NewDeliverWorker(store)
	if err := worker.Work(ctx, &river.Job[DeliverArgs]{Args: DeliverArgs{DeliveryID: deliveryID}}); err != nil {
		t.Fatalf("worker.Work: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if gotEvent != "drill.completed" {
		t.Fatalf("event header = %q, want drill.completed", gotEvent)
	}
	if !Verify(endpoint.Secret, gotBody, gotSig) {
		t.Fatalf("signature %q does not verify against received body", gotSig)
	}

	d, err := store.GetDelivery(ctx, deliveryID)
	if err != nil {
		t.Fatalf("get delivery: %v", err)
	}
	if d.Status != StatusDelivered {
		t.Fatalf("delivery status = %s, want delivered", d.Status)
	}
	if d.AttemptCount != 1 {
		t.Fatalf("attempt count = %d, want 1", d.AttemptCount)
	}
	if d.LastStatusCode == nil || *d.LastStatusCode != 200 {
		t.Fatalf("last status code = %v, want 200", d.LastStatusCode)
	}
}

func TestWebhookDeliveryFailureMarksFailed(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()

	store := NewStore(pool)
	accountID := seedAccount(t, ctx, pool)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	endpoint, err := store.CreateEndpoint(ctx, accountID, srv.URL)
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	payload, _ := MarshalPayload("drill.failed", accountID.String(), nil)
	deliveryID, err := store.CreateDelivery(ctx, endpoint.ID, accountID, "drill.failed", payload)
	if err != nil {
		t.Fatalf("create delivery: %v", err)
	}

	worker := NewDeliverWorker(store)
	// A non-2xx response must return an error so River retries.
	if err := worker.Work(ctx, &river.Job[DeliverArgs]{Args: DeliverArgs{DeliveryID: deliveryID}}); err == nil {
		t.Fatal("worker.Work should return an error on a 500 response")
	}

	d, err := store.GetDelivery(ctx, deliveryID)
	if err != nil {
		t.Fatalf("get delivery: %v", err)
	}
	if d.Status != StatusFailed {
		t.Fatalf("delivery status = %s, want failed", d.Status)
	}
	if d.LastStatusCode == nil || *d.LastStatusCode != 500 {
		t.Fatalf("last status code = %v, want 500", d.LastStatusCode)
	}
}

func TestDispatchFanOut(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()

	store := NewStore(pool)
	accountID := seedAccount(t, ctx, pool)

	for i := 0; i < 2; i++ {
		if _, err := store.CreateEndpoint(ctx, accountID, "https://example.com/hook"); err != nil {
			t.Fatalf("create endpoint: %v", err)
		}
	}

	fake := &fakeInserter{}
	d := NewDispatcher(store, fake)
	if err := d.Dispatch(ctx, accountID, "drill.completed", map[string]any{"k": "v"}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	// One delivery row + one enqueue per enabled endpoint.
	if fake.count != 2 {
		t.Fatalf("enqueued %d jobs, want 2", fake.count)
	}
	endpoints, _ := store.ListEndpoints(ctx, accountID)
	total := 0
	for _, e := range endpoints {
		ds, _ := store.ListDeliveries(ctx, accountID, e.ID, 10)
		total += len(ds)
	}
	if total != 2 {
		t.Fatalf("created %d delivery rows, want 2", total)
	}
}

type fakeInserter struct{ count int }

func (f *fakeInserter) Insert(_ context.Context, _ river.JobArgs, _ *river.InsertOpts) (*rivertype.JobInsertResult, error) {
	f.count++
	return &rivertype.JobInsertResult{}, nil
}
