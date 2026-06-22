package alerting

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preshotcome/dokaz/internal/drill"
	"github.com/preshotcome/dokaz/internal/heartbeat"
)

func TestNotifier(t *testing.T) {
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

	accountID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO accounts (id,name,slug,plan) VALUES ($1,'a','a-'||$2,'pro')`,
		accountID, accountID.String()[:8]); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE id=$1`, accountID) })

	// Capture servers for Slack + PagerDuty.
	var slackBodies, pdBodies []map[string]any
	capture := func(dst *[]map[string]any) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			var m map[string]any
			_ = json.Unmarshal(b, &m)
			*dst = append(*dst, m)
			w.WriteHeader(200)
		}
	}
	slackSrv := httptest.NewServer(capture(&slackBodies))
	defer slackSrv.Close()
	pdSrv := httptest.NewServer(capture(&pdBodies))
	defer pdSrv.Close()
	pagerDutyEnqueueURL = pdSrv.URL // overridden for the test

	store := NewStore(pool)
	if err := store.Set(ctx, accountID, Channels{SlackWebhookURL: slackSrv.URL, PagerDutyRoutingKey: "rk_test"}); err != nil {
		t.Fatalf("set channels: %v", err)
	}
	n := New(store, slog.Default())

	// A failed drill fires both channels.
	if err := n.NotifyDrill(ctx, drill.Drill{ID: uuid.New(), AccountID: accountID}, drill.EventFailed, "table missing"); err != nil {
		t.Fatalf("NotifyDrill: %v", err)
	}
	if len(slackBodies) != 1 || len(pdBodies) != 1 {
		t.Fatalf("drill-failed: slack=%d pd=%d, want 1/1", len(slackBodies), len(pdBodies))
	}
	if pdBodies[0]["event_action"] != "trigger" {
		t.Errorf("drill-failed pagerduty event_action = %v, want trigger", pdBodies[0]["event_action"])
	}

	// A passing drill is silent.
	slackBodies, pdBodies = nil, nil
	_ = n.NotifyDrill(ctx, drill.Drill{ID: uuid.New(), AccountID: accountID}, drill.EventCompleted, "")
	if len(slackBodies) != 0 || len(pdBodies) != 0 {
		t.Errorf("passing drill should be silent, got slack=%d pd=%d", len(slackBodies), len(pdBodies))
	}

	// Heartbeat down → trigger; up → resolve.
	slackBodies, pdBodies = nil, nil
	hb := heartbeat.Heartbeat{ID: uuid.New(), AccountID: accountID, Name: "nightly-dump"}
	_ = n.Notify(ctx, hb, heartbeat.EventDown)
	_ = n.Notify(ctx, hb, heartbeat.EventUp)
	if len(pdBodies) != 2 {
		t.Fatalf("heartbeat down+up: pd=%d, want 2", len(pdBodies))
	}
	if pdBodies[0]["event_action"] != "trigger" || pdBodies[1]["event_action"] != "resolve" {
		t.Errorf("heartbeat actions = %v/%v, want trigger/resolve", pdBodies[0]["event_action"], pdBodies[1]["event_action"])
	}

	// Unconfigured account → no-op.
	other := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO accounts (id,name,slug,plan) VALUES ($1,'b','b-'||$2,'pro')`,
		other, other.String()[:8]); err != nil {
		t.Fatalf("seed other: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE id=$1`, other) })
	slackBodies, pdBodies = nil, nil
	_ = n.NotifyDrill(ctx, drill.Drill{ID: uuid.New(), AccountID: other}, drill.EventFailed, "x")
	if len(slackBodies) != 0 || len(pdBodies) != 0 {
		t.Errorf("unconfigured account should not alert, got slack=%d pd=%d", len(slackBodies), len(pdBodies))
	}
}
