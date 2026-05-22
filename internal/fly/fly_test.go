package fly

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateMachine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("missing bearer token: %q", r.Header.Get("Authorization"))
		}
		if r.Method != http.MethodPost || r.URL.Path != "/apps/drills/machines" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"image":"postgres:16"`) {
			t.Errorf("create body missing image: %s", body)
		}
		_, _ = io.WriteString(w, `{"id":"148e2","state":"created","private_ip":"fdaa::3"}`)
	}))
	defer srv.Close()

	c := &Client{app: "drills", token: "tok", base: srv.URL, http: srv.Client()}
	m, err := c.CreateMachine(context.Background(), CreateInput{
		Image: "postgres:16", Region: "iad", MemoryMB: 1024, CPUs: 1,
		Env: map[string]string{"POSTGRES_PASSWORD": "x"},
	})
	if err != nil {
		t.Fatalf("CreateMachine: %v", err)
	}
	if m.ID != "148e2" || m.PrivateIP != "fdaa::3" {
		t.Fatalf("machine = %+v", m)
	}
}

func TestWaitAndDestroy(t *testing.T) {
	var waited, destroyed bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/wait"):
			waited = true
		case r.Method == http.MethodDelete:
			destroyed = true
			if r.URL.Query().Get("force") != "true" {
				t.Error("destroy should force")
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := &Client{app: "drills", token: "tok", base: srv.URL, http: srv.Client()}
	if err := c.WaitStarted(context.Background(), "148e2"); err != nil {
		t.Fatalf("WaitStarted: %v", err)
	}
	if err := c.Destroy(context.Background(), "148e2"); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if !waited || !destroyed {
		t.Fatalf("waited=%v destroyed=%v", waited, destroyed)
	}
}

func TestDestroyToleratesMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()
	c := &Client{app: "drills", token: "tok", base: srv.URL, http: srv.Client()}
	if err := c.Destroy(context.Background(), "gone"); err != nil {
		t.Fatalf("Destroy of a missing machine should be a no-op, got %v", err)
	}
}
