// Package fly is a small client for the Fly.io Machines API — enough to
// provision and destroy a per-drill sandbox VM. It talks to the REST API
// directly over net/http (Bearer-token auth); no SDK.
package fly

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client calls the Fly Machines API for one Fly app.
type Client struct {
	app   string
	token string
	base  string
	http  *http.Client
}

// NewClient builds a client for the given Fly app and API token.
func NewClient(app, token string) *Client {
	return &Client{
		app:   app,
		token: token,
		base:  "https://api.machines.dev/v1",
		http:  &http.Client{Timeout: 30 * time.Second},
	}
}

// Machine is a Fly Machine as the API reports it.
type Machine struct {
	ID        string `json:"id"`
	State     string `json:"state"`
	PrivateIP string `json:"private_ip"`
}

// CreateInput describes a machine to create.
type CreateInput struct {
	Image    string
	Env      map[string]string
	Region   string
	MemoryMB int
	CPUs     int
}

// CreateMachine provisions a new machine and returns it (typically in the
// "created"/"starting" state — call WaitStarted next).
func (c *Client) CreateMachine(ctx context.Context, in CreateInput) (Machine, error) {
	guest := map[string]any{"cpu_kind": "shared", "cpus": in.CPUs, "memory_mb": in.MemoryMB}
	body := map[string]any{
		"region": in.Region,
		"config": map[string]any{
			"image":        in.Image,
			"env":          in.Env,
			"guest":        guest,
			"auto_destroy": true, // a crashed machine cleans itself up
		},
	}
	var m Machine
	if err := c.do(ctx, http.MethodPost, "/apps/"+c.app+"/machines", body, &m); err != nil {
		return Machine{}, err
	}
	return m, nil
}

// WaitStarted blocks until the machine reaches the "started" state, using
// Fly's server-side wait endpoint.
func (c *Client) WaitStarted(ctx context.Context, machineID string) error {
	return c.do(ctx, http.MethodGet,
		"/apps/"+c.app+"/machines/"+machineID+"/wait?state=started&timeout=60", nil, nil)
}

// Destroy force-deletes a machine. A 404 (already gone) is not an error.
func (c *Client) Destroy(ctx context.Context, machineID string) error {
	return c.do(ctx, http.MethodDelete,
		"/apps/"+c.app+"/machines/"+machineID+"?force=true", nil, nil)
}

// do issues an authenticated request, decoding a JSON response into out when
// out is non-nil.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("fly: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusNotFound && method == http.MethodDelete {
		return nil
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("fly: %s %s: %s: %s", method, path, resp.Status, snippet(payload))
	}
	if out != nil && len(payload) > 0 {
		if err := json.Unmarshal(payload, out); err != nil {
			return fmt.Errorf("fly: decode %s response: %w", path, err)
		}
	}
	return nil
}

func snippet(b []byte) string {
	s := string(b)
	if len(s) > 300 {
		return s[:300]
	}
	return s
}
