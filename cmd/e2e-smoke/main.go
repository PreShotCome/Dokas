// Command e2e-smoke walks a real signup → connect → assert → run drill →
// download PDF flow against a running Selket server and exits 0 only if
// the resulting PDF reads "Verdict: PASSED". Wired into CI to catch the
// class of pre-launch bugs that hit us five times in one onboarding
// session - each individually-OK piece passes its own unit tests, but
// the full flow exposes the seams between them:
//
//   - schema/code drift between SELECT and rows.Scan
//   - CHECK constraints not updated alongside new enum values
//   - PDFs stamped FAILED for passing drills (in-memory status drift)
//   - ephemeral keys rotating per restart, breaking decrypt
//   - mojibake in PDF text from UTF-8 chars in a Latin-1 font
//
// Usage:
//
//	BASE_URL=http://localhost:5173 \
//	  E2E_DUMP_PATH=tmp/sources/test.dump \
//	  go run ./cmd/e2e-smoke
package main

import (
	"bytes"
	"compress/zlib"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

const csrfField = "_csrf"

var (
	csrfRE = regexp.MustCompile(`name="_csrf"\s+value="([^"]+)"`)
	// Drill detail page renders the current status inside
	// <dd id="drill-status">@StatusBadge(...)</dd>; StatusBadge wraps the
	// raw status text in a single <span>. Match the inner text.
	statusRE = regexp.MustCompile(`id="drill-status"[^>]*>[\s\S]*?>(succeeded|failed|running|pending|skipped)<`)
)

func main() {
	base := strings.TrimRight(env("BASE_URL", "http://localhost:5173"), "/")

	stamp := randHex(6)
	email := fmt.Sprintf("e2e-%s@local.test", stamp)
	password := "e2e-smoke-pass-12345!"

	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, Timeout: 30 * time.Second}

	// The smoke walks the free-user happy path: sign up, then run a drill
	// against the built-in sample dataset (a real pg_dump exercised through
	// the full provision -> restore -> assert -> report -> sign pipeline).
	// The sample target ships with a table_exists assertion - the kind that
	// surfaced the assertion_results CHECK-constraint bug - so the smoke
	// still covers that seam without a free account needing a paid plan.
	step("signup", func() error { return signup(c, base, email, password) })
	var drillID string
	step("run sample drill", func() (err error) {
		drillID, err = runSampleDrill(c, base)
		return
	})
	step("wait for drill completion", func() error {
		return waitForDrill(c, base, drillID, 90*time.Second)
	})
	var pdf []byte
	step("download evidence PDF", func() (err error) {
		pdf, err = downloadPDF(c, base, drillID)
		return
	})

	// PDF text streams are deflate-compressed; pull the readable text out
	// before searching so the assertion isn't fooled by raw compressed bytes.
	pdfText := inflatePDFStreams(pdf)
	if !bytes.Contains(pdfText, []byte("Verdict: PASSED")) {
		die("PDF does not contain 'Verdict: PASSED' - report-render regression\nInflated text:\n%s",
			truncate(pdfText, 800))
	}
	// Mojibake detector: the three-byte sequence "â€" is Windows-1252
	// reading the first two bytes of a UTF-8 em-dash. If it shows up in
	// the PDF text streams, a non-ASCII character snuck back into the
	// renderer string literals.
	if bytes.Contains(pdfText, []byte{0xc3, 0xa2, 0xe2, 0x82, 0xac}) {
		die("PDF contains mojibake (Latin-1 reading of UTF-8) - non-ASCII back in pdf.go literals")
	}

	fmt.Printf("\n  ALL GREEN  (drill %s, %d byte PDF)\n", drillID, len(pdf))
}

// --- flow helpers ---

func signup(c *http.Client, base, email, password string) error {
	token, err := getCSRF(c, base+"/signup")
	if err != nil {
		return fmt.Errorf("fetch /signup: %w", err)
	}
	form := url.Values{csrfField: {token}, "email": {email}, "password": {password}}
	resp, err := c.PostForm(base+"/signup", form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("/signup returned %d:\n%s", resp.StatusCode, truncate(body, 500))
	}
	return nil
}

// runSampleDrill posts the sample-drill form (the free-user demo) and returns
// the new drill's ID, parsed from the /drills/{id} page it redirects to. The
// CSRF token comes from the /databases page, which renders the sample-drill
// form for any account.
func runSampleDrill(c *http.Client, base string) (string, error) {
	token, err := getCSRF(c, base+"/databases")
	if err != nil {
		return "", fmt.Errorf("fetch /databases: %w", err)
	}
	form := url.Values{csrfField: {token}}
	resp, err := c.PostForm(base+"/databases/sample-drill", form)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("/databases/sample-drill returned %d:\n%s", resp.StatusCode, truncate(body, 500))
	}
	final := resp.Request.URL.Path
	parts := strings.Split(final, "/")
	if len(parts) < 3 || parts[1] != "drills" {
		return "", fmt.Errorf("expected redirect to /drills/{id}, got %q", final)
	}
	return parts[2], nil
}

func waitForDrill(c *http.Client, base, drillID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		body, err := getHTML(c, base+"/drills/"+drillID)
		if err != nil {
			return err
		}
		if m := statusRE.FindSubmatch(body); m != nil {
			switch string(m[1]) {
			case "succeeded":
				return nil
			case "failed":
				return fmt.Errorf("drill ended with status: failed")
			}
		}
		time.Sleep(750 * time.Millisecond)
	}
	return fmt.Errorf("drill did not finish within %s", timeout)
}

func downloadPDF(c *http.Client, base, drillID string) ([]byte, error) {
	resp, err := c.Get(base + "/drills/" + drillID + "/evidence")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("evidence endpoint returned %d:\n%s", resp.StatusCode, truncate(body, 400))
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/pdf") {
		return nil, fmt.Errorf("evidence content-type is %q, expected application/pdf",
			resp.Header.Get("Content-Type"))
	}
	return io.ReadAll(resp.Body)
}

// --- plumbing ---

func getCSRF(c *http.Client, url string) (string, error) {
	body, err := getHTML(c, url)
	if err != nil {
		return "", err
	}
	m := csrfRE.FindSubmatch(body)
	if m == nil {
		return "", fmt.Errorf("no _csrf input found on %s", url)
	}
	return string(m[1]), nil
}

func getHTML(c *http.Client, url string) ([]byte, error) {
	resp, err := c.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("GET %s returned %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func step(name string, fn func() error) {
	fmt.Printf("==> %s ... ", name)
	start := time.Now()
	err := fn()
	dur := time.Since(start).Round(time.Millisecond)
	if err != nil {
		fmt.Printf("FAIL  (%s)\n\n", dur)
		die("%s: %v", name, err)
	}
	fmt.Printf("ok  (%s)\n", dur)
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

// inflatePDFStreams finds every `stream\n...\nendstream` block in a PDF,
// attempts a zlib inflate, and returns the concatenated decompressed text
// alongside the raw bytes. Streams that aren't deflate-compressed (or
// that fail to decode) are passed through raw, so the search still
// catches uncompressed PDFs and any text that lives outside streams.
func inflatePDFStreams(pdf []byte) []byte {
	var out bytes.Buffer
	out.Write(pdf) // keep the raw bytes too — non-stream text is searchable.
	out.WriteByte('\n')

	for i := 0; i < len(pdf); {
		start := bytes.Index(pdf[i:], []byte("stream\n"))
		if start < 0 {
			break
		}
		start += i + len("stream\n")
		end := bytes.Index(pdf[start:], []byte("\nendstream"))
		if end < 0 {
			break
		}
		blob := pdf[start : start+end]
		r, err := zlib.NewReader(bytes.NewReader(blob))
		if err == nil {
			b, err := io.ReadAll(r)
			r.Close()
			if err == nil {
				out.Write(b)
				out.WriteByte('\n')
			}
		}
		i = start + end + len("\nendstream")
	}
	return out.Bytes()
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
