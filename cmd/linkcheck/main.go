// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

// Command linkcheck crawls a running Dokaz server and fails if any page
// errors or any internal link is broken. It complements cmd/e2e-smoke
// (which proves the signup -> drill -> PDF flow): linkcheck proves the
// chrome around that flow renders and every <a href> / <link> / <script>
// the templates emit actually resolves.
//
// It signs up a throwaway account first (unless LINKCHECK_PUBLIC_ONLY=1)
// so the authenticated nav — dashboard, databases, drills, check-ins,
// reports, account, billing — is crawled too, not just the marketing
// pages. Only GET is issued; forms (POST) and /logout are never followed,
// so the crawl never mutates state beyond the one signup.
//
// Usage:
//
//	BASE_URL=http://127.0.0.1:8099 go run ./cmd/linkcheck
package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

const csrfField = "_csrf"

var (
	csrfRE = regexp.MustCompile(`name="_csrf"\s+value="([^"]+)"`)
	// Pull href/src targets out of rendered HTML. Good enough for this
	// server-rendered, no-SPA site — no need to pull in an HTML parser.
	linkRE = regexp.MustCompile(`(?:href|src)="([^"]+)"`)
)

// skipPath matches GET URLs that are valid but shouldn't be crawled:
// logout is POST-only chrome, the static bundle is not HTML, and the
// export/evidence endpoints stream files rather than pages. They are still
// reachable; we just don't recurse into them.
var skipPrefixes = []string{
	"/logout", "/impersonate/stop", "/static/",
	"/account/export", "/openapi.json",
}

func main() {
	base := strings.TrimRight(env("BASE_URL", "http://127.0.0.1:8099"), "/")
	baseURL, err := url.Parse(base)
	if err != nil {
		die("bad BASE_URL %q: %v", base, err)
	}

	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, Timeout: 30 * time.Second}

	// Seed the crawl. Public pages always; the signed-in nav once we have a
	// session. The signup gives the crawler a real, unlapsed trial account so
	// the authenticated pages render their full chrome.
	seeds := []string{"/", "/pricing", "/how-it-works", "/help", "/docs",
		"/login", "/signup", "/legal/terms", "/legal/privacy",
		"/legal/dpa", "/legal/subprocessors", "/legal/cookies"}

	if os.Getenv("LINKCHECK_PUBLIC_ONLY") != "1" {
		if err := signup(c, base); err != nil {
			die("signup (needed for authed crawl; set LINKCHECK_PUBLIC_ONLY=1 to skip): %v", err)
		}
		fmt.Println("==> signed up throwaway account for authenticated crawl")
		seeds = append(seeds, "/dashboard", "/databases", "/drills",
			"/heartbeats", "/reports", "/account", "/account/api-keys",
			"/account/webhooks", "/account/audit", "/account/mfa")
	} else {
		fmt.Println("==> LINKCHECK_PUBLIC_ONLY=1 — crawling signed-out pages only")
	}

	cr := &crawler{client: c, base: baseURL, seen: map[string]bool{}, broken: map[string]string{}}
	for _, s := range seeds {
		cr.enqueue(s, "<seed>")
	}
	cr.run()

	fmt.Printf("\n  crawled %d pages, checked %d links\n", cr.pages, cr.checked)
	if len(cr.broken) == 0 {
		fmt.Printf("  ALL LINKS OK\n")
		return
	}
	urls := make([]string, 0, len(cr.broken))
	for u := range cr.broken {
		urls = append(urls, u)
	}
	sort.Strings(urls)
	fmt.Fprintf(os.Stderr, "\n!! %d broken link(s):\n", len(cr.broken))
	for _, u := range urls {
		fmt.Fprintf(os.Stderr, "   %s  (%s)\n", u, cr.broken[u])
	}
	os.Exit(1)
}

type crawler struct {
	client  *http.Client
	base    *url.URL
	seen    map[string]bool
	broken  map[string]string // path -> reason (deduped)
	queue   []queued
	pages   int
	checked int
}

type queued struct{ path, from string }

func (cr *crawler) enqueue(path, from string) {
	p := cr.normalize(path)
	if p == "" || cr.seen[p] {
		return
	}
	cr.seen[p] = true
	cr.queue = append(cr.queue, queued{p, from})
}

// normalize keeps only same-host, crawlable GET paths. External links,
// mailto/tel, pure anchors, and the skip list are dropped (returns "").
func (cr *crawler) normalize(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "#") ||
		strings.HasPrefix(raw, "mailto:") || strings.HasPrefix(raw, "tel:") ||
		strings.HasPrefix(raw, "data:") || strings.HasPrefix(raw, "javascript:") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	// Resolve relative against the base; reject other hosts.
	abs := cr.base.ResolveReference(u)
	if abs.Host != cr.base.Host {
		return ""
	}
	p := abs.Path
	for _, sp := range skipPrefixes {
		if strings.HasPrefix(p, sp) {
			return ""
		}
	}
	return p
}

func (cr *crawler) run() {
	for len(cr.queue) > 0 {
		item := cr.queue[0]
		cr.queue = cr.queue[1:]
		cr.checked++

		resp, err := cr.client.Get(cr.base.String() + item.path)
		if err != nil {
			cr.broken[item.path] = fmt.Sprintf("GET error: %v (linked from %s)", err, item.from)
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()

		if resp.StatusCode >= 400 {
			cr.broken[item.path] = fmt.Sprintf("HTTP %d (linked from %s)", resp.StatusCode, item.from)
			continue
		}

		// Only recurse into HTML pages — skip PDFs, CSVs, JSON, etc.
		if !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
			continue
		}
		cr.pages++
		for _, m := range linkRE.FindAllStringSubmatch(string(body), -1) {
			cr.enqueue(m[1], item.path)
		}
	}
}

// signup registers a throwaway account so the crawler holds a session for
// the authenticated pages. Mirrors cmd/e2e-smoke's signup.
func signup(c *http.Client, base string) error {
	token, err := getCSRF(c, base+"/signup")
	if err != nil {
		return fmt.Errorf("fetch /signup: %w", err)
	}
	email := fmt.Sprintf("linkcheck-%s@local.test", randHex(6))
	form := url.Values{csrfField: {token}, "email": {email}, "password": {"linkcheck-pass-12345!"}}
	resp, err := c.PostForm(base+"/signup", form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("/signup returned %d:\n%s", resp.StatusCode, truncate(body, 400))
	}
	return nil
}

func getCSRF(c *http.Client, u string) (string, error) {
	resp, err := c.Get(u)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	m := csrfRE.FindSubmatch(body)
	if m == nil {
		return "", fmt.Errorf("no _csrf input on %s", u)
	}
	return string(m[1]), nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func truncate(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return b[:n]
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "\n!! "+format+"\n", args...)
	os.Exit(1)
}
