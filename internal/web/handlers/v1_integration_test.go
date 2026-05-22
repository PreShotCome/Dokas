package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/preshotcome/anything/internal/account"
	"github.com/preshotcome/anything/internal/apikey"
	"github.com/preshotcome/anything/internal/audit"
	"github.com/preshotcome/anything/internal/drill"
	"github.com/preshotcome/anything/internal/evidence"
	"github.com/preshotcome/anything/internal/ratelimit"
)

// fakeInserter satisfies drill.RiverInserter without a real River client —
// drill rows are still written to the DB; only the job enqueue is faked.
type v1FakeInserter struct{}

func (v1FakeInserter) Insert(context.Context, river.JobArgs, *river.InsertOpts) (*rivertype.JobInsertResult, error) {
	return &rivertype.JobInsertResult{}, nil
}

// v1TestServer builds a Handlers with just the fields the /v1 router needs,
// seeds an account, and returns the server, a full-access API key, the
// account + user IDs, and the key store so a test can mint scoped keys.
func v1TestServer(t *testing.T, pool *pgxpool.Pool) (*httptest.Server, string, uuid.UUID, uuid.UUID, *apikey.Store) {
	t.Helper()
	ctx := context.Background()

	userID := uuid.New()
	accountID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id,email,password_hash) VALUES ($1,$2,'x')`,
		userID, "v1-"+userID.String()+"@example.com"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	// Seed on the Pro tier so plan limits don't constrain the API tests —
	// limit enforcement has its own dedicated coverage.
	if _, err := pool.Exec(ctx, `INSERT INTO accounts (id,name,slug,plan) VALUES ($1,'v1','v1-'||$2,'pro')`,
		accountID, accountID.String()[:8]); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE id=$1`, accountID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})

	apiKeys := apikey.NewStore(pool)
	key, err := apiKeys.Create(ctx, accountID, userID, "test", apikey.AllScopes)
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	signer, _ := evidence.NewSigner("")
	evCipher, err := evidence.NewCipher("", pool)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	h := &Handlers{
		pool:      pool,
		drills:    drill.NewStore(pool),
		accounts:  account.NewStore(pool),
		apiKeys:   apiKeys,
		orch:      drill.NewOrchestrator(drill.NewStore(pool), v1FakeInserter{}, audit.New(pool)),
		evidence:  evidence.NewService(evidence.NewLocalStore(t.TempDir()), signer, evCipher, pool),
		v1Limiter: ratelimit.New(10000, 10000), // effectively unlimited for tests
		// Confine source paths to the testdata fixtures directory.
		sourceDir: filepath.Dir(mustAbsTestdata(t)),
	}
	srv := httptest.NewServer(h.v1Router())
	t.Cleanup(srv.Close)
	return srv, key.Secret, accountID, userID, apiKeys
}

func v1Do(t *testing.T, method, url, apiKey, idemKey, body string) (*http.Response, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if idemKey != "" {
		req.Header.Set("Idempotency-Key", idemKey)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var env map[string]any
	if len(raw) > 0 && raw[0] == '{' {
		_ = json.Unmarshal(raw, &env)
	}
	return resp, env
}

func TestV1Auth(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()
	srv, key, _, _, _ := v1TestServer(t, pool)

	// No key → 401.
	if resp, _ := v1Do(t, "GET", srv.URL+"/databases", "", "", ""); resp.StatusCode != 401 {
		t.Fatalf("no key: got %d, want 401", resp.StatusCode)
	}
	// Garbage key → 401.
	if resp, _ := v1Do(t, "GET", srv.URL+"/databases", "so_garbage", "", ""); resp.StatusCode != 401 {
		t.Fatalf("bad key: got %d, want 401", resp.StatusCode)
	}
	// Valid key → 200, empty data list.
	resp, env := v1Do(t, "GET", srv.URL+"/databases", key, "", "")
	if resp.StatusCode != 200 {
		t.Fatalf("valid key: got %d, want 200", resp.StatusCode)
	}
	if _, ok := env["data"]; !ok {
		t.Fatalf("response missing data envelope: %v", env)
	}
}

func TestV1Scopes(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()
	srv, _, accountID, userID, apiKeys := v1TestServer(t, pool)
	ctx := context.Background()

	// A read-only key can list databases but cannot create them.
	ro, err := apiKeys.Create(ctx, accountID, userID, "read-only",
		[]string{apikey.ScopeDatabasesRead, apikey.ScopeDrillsRead})
	if err != nil {
		t.Fatalf("create read-only key: %v", err)
	}
	if resp, _ := v1Do(t, "GET", srv.URL+"/databases", ro.Secret, "", ""); resp.StatusCode != 200 {
		t.Fatalf("read-only GET /databases: got %d, want 200", resp.StatusCode)
	}
	body := `{"name":"prod","source_uri":"` + mustAbsTestdata(t) + `"}`
	if resp, env := v1Do(t, "POST", srv.URL+"/databases", ro.Secret, "scope-1", body); resp.StatusCode != 403 {
		t.Fatalf("read-only POST /databases: got %d, want 403 (env=%v)", resp.StatusCode, env)
	}

	// A drills-only key cannot reach the database endpoints at all.
	drillsOnly, err := apiKeys.Create(ctx, accountID, userID, "drills-only",
		[]string{apikey.ScopeDrillsRead})
	if err != nil {
		t.Fatalf("create drills-only key: %v", err)
	}
	if resp, _ := v1Do(t, "GET", srv.URL+"/databases", drillsOnly.Secret, "", ""); resp.StatusCode != 403 {
		t.Fatalf("drills-only GET /databases: got %d, want 403", resp.StatusCode)
	}
	if resp, _ := v1Do(t, "GET", srv.URL+"/drills", drillsOnly.Secret, "", ""); resp.StatusCode != 200 {
		t.Fatalf("drills-only GET /drills: got %d, want 200", resp.StatusCode)
	}
}

func TestV1DatabaseCreateAndIdempotency(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()
	srv, key, _, _, _ := v1TestServer(t, pool)

	fixture := mustAbsTestdata(t)
	body := `{"name":"prod","source_uri":"` + fixture + `","assertions":[{"kind":"row_count","config":{"table":"events","min_rows":1}}]}`

	// POST without Idempotency-Key → 400.
	if resp, _ := v1Do(t, "POST", srv.URL+"/databases", key, "", body); resp.StatusCode != 400 {
		t.Fatalf("no idempotency key: got %d, want 400", resp.StatusCode)
	}

	// POST with key → 201.
	resp, env := v1Do(t, "POST", srv.URL+"/databases", key, "idem-1", body)
	if resp.StatusCode != 201 {
		t.Fatalf("create: got %d, want 201 (env=%v)", resp.StatusCode, env)
	}
	data, _ := env["data"].(map[string]any)
	dbID, _ := data["id"].(string)
	if dbID == "" {
		t.Fatalf("created database has no id: %v", env)
	}
	if as, _ := data["assertions"].([]any); len(as) != 1 {
		t.Fatalf("created database should carry 1 assertion, got %v", data["assertions"])
	}

	// Replay same key + same body → 200, replayed header.
	resp2, _ := v1Do(t, "POST", srv.URL+"/databases", key, "idem-1", body)
	if resp2.StatusCode != 201 {
		t.Fatalf("replay: got %d, want the stored 201", resp2.StatusCode)
	}
	if resp2.Header.Get("Idempotency-Replayed") != "true" {
		t.Fatal("replay should carry Idempotency-Replayed: true")
	}

	// Same key, different body → 409.
	if resp3, _ := v1Do(t, "POST", srv.URL+"/databases", key, "idem-1",
		`{"name":"other","source_uri":"`+fixture+`"}`); resp3.StatusCode != 409 {
		t.Fatalf("key reuse with different body: got %d, want 409", resp3.StatusCode)
	}

	// GET the database back.
	if resp, _ := v1Do(t, "GET", srv.URL+"/databases/"+dbID, key, "", ""); resp.StatusCode != 200 {
		t.Fatalf("get database: got %d, want 200", resp.StatusCode)
	}
	// A non-existent database → 404.
	if resp, _ := v1Do(t, "GET", srv.URL+"/databases/"+uuid.NewString(), key, "", ""); resp.StatusCode != 404 {
		t.Fatalf("missing database: got %d, want 404", resp.StatusCode)
	}
}

func TestV1DrillCreateAndList(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()
	srv, key, _, _, _ := v1TestServer(t, pool)

	fixture := mustAbsTestdata(t)
	_, env := v1Do(t, "POST", srv.URL+"/databases", key, "db-1",
		`{"name":"prod","source_uri":"`+fixture+`"}`)
	dbID := env["data"].(map[string]any)["id"].(string)

	// Start a drill.
	resp, denv := v1Do(t, "POST", srv.URL+"/drills", key, "drill-1", `{"database_id":"`+dbID+`"}`)
	if resp.StatusCode != 201 {
		t.Fatalf("create drill: got %d, want 201 (env=%v)", resp.StatusCode, denv)
	}
	drillData := denv["data"].(map[string]any)
	if drillData["database_id"] != dbID {
		t.Fatalf("drill database_id = %v, want %s", drillData["database_id"], dbID)
	}

	// It appears in the list.
	_, lenv := v1Do(t, "GET", srv.URL+"/drills", key, "", "")
	drills, _ := lenv["data"].([]any)
	if len(drills) != 1 {
		t.Fatalf("drill list has %d items, want 1", len(drills))
	}
	meta, _ := lenv["meta"].(map[string]any)
	if meta["count"].(float64) != 1 {
		t.Fatalf("meta.count = %v, want 1", meta["count"])
	}
}

func TestV1Pagination(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()
	srv, key, _, _, _ := v1TestServer(t, pool)

	fixture := mustAbsTestdata(t)
	for i := 0; i < 3; i++ {
		body := `{"name":"db","source_uri":"` + fixture + `"}`
		v1Do(t, "POST", srv.URL+"/databases", key, "page-"+uuid.NewString(), body)
	}

	// First page of 2 → a next_cursor.
	_, env := v1Do(t, "GET", srv.URL+"/databases?limit=2", key, "", "")
	page1, _ := env["data"].([]any)
	meta, _ := env["meta"].(map[string]any)
	if len(page1) != 2 {
		t.Fatalf("page 1 has %d items, want 2", len(page1))
	}
	cursor, _ := meta["next_cursor"].(string)
	if cursor == "" {
		t.Fatal("page 1 should have a next_cursor")
	}

	// Following the cursor yields the remaining row.
	_, env2 := v1Do(t, "GET", srv.URL+"/databases?limit=2&cursor="+cursor, key, "", "")
	page2, _ := env2["data"].([]any)
	if len(page2) != 1 {
		t.Fatalf("page 2 has %d items, want 1", len(page2))
	}
	meta2, _ := env2["meta"].(map[string]any)
	if nc, _ := meta2["next_cursor"].(string); nc != "" {
		t.Fatalf("page 2 should be the last page, got next_cursor %q", nc)
	}
}

// TestV1DatabasePlanLimit verifies the subscription-tier cap is enforced on
// the /v1 database create path: a trial account gets one database, the next
// POST is rejected with 403 plan_limit.
func TestV1DatabasePlanLimit(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()
	srv, key, accountID, _, _ := v1TestServer(t, pool)

	// v1TestServer seeds Pro; drop to trial — one database allowed.
	if _, err := pool.Exec(context.Background(),
		`UPDATE accounts SET plan='trial' WHERE id=$1`, accountID); err != nil {
		t.Fatalf("set plan: %v", err)
	}

	fixture := mustAbsTestdata(t)
	body := `{"name":"db","source_uri":"` + fixture + `"}`

	// First database — within the trial cap.
	if resp, _ := v1Do(t, "POST", srv.URL+"/databases", key, uuid.NewString(), body); resp.StatusCode != 201 {
		t.Fatalf("first create: got %d, want 201", resp.StatusCode)
	}
	// Second — over the cap.
	resp, env := v1Do(t, "POST", srv.URL+"/databases", key, uuid.NewString(), body)
	if resp.StatusCode != 403 {
		t.Fatalf("second create: got %d, want 403", resp.StatusCode)
	}
	errs, _ := env["errors"].([]any)
	if len(errs) == 0 {
		t.Fatal("403 response should carry an error")
	}
	first, _ := errs[0].(map[string]any)
	if code, _ := first["code"].(string); code != "plan_limit" {
		t.Fatalf("error code = %q, want plan_limit", code)
	}
}

func mustAbsTestdata(t *testing.T) string {
	t.Helper()
	// The drill fixture, resolved relative to this package's directory.
	p, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return p + "/../../../testdata/fixtures/tiny.dump"
}
