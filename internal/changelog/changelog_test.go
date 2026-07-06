// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

package changelog

import "testing"

const sample = `# Changelog

Intro paragraph with a [link](https://example.com) — ignored by the parser.

## [2026-07-06]

### Added
- **Teams** — an account can partition its ` + "`databases`" + ` into teams.
  A wrapped continuation line.
- Another item with a [doc link](docs/backlog.md).

### Fixed
- A single fix.

## [2026-05-20]

- Project began as **Restore Drill**.
`

func TestParse(t *testing.T) {
	doc := Parse(sample)
	if len(doc.Releases) != 2 {
		t.Fatalf("got %d releases, want 2", len(doc.Releases))
	}

	r0 := doc.Releases[0]
	if r0.Heading != "2026-07-06" {
		t.Fatalf("heading = %q, want 2026-07-06 (brackets stripped)", r0.Heading)
	}
	if len(r0.Groups) != 2 {
		t.Fatalf("release 0 groups = %d, want 2", len(r0.Groups))
	}
	added := r0.Groups[0]
	if added.Title != "Added" || len(added.Items) != 2 {
		t.Fatalf("Added group = %q with %d items, want Added/2", added.Title, len(added.Items))
	}

	// First item: bold, text, code, text — and the continuation line joined.
	segs := added.Items[0].Segments
	if segs[0].Kind != KindBold || segs[0].Text != "Teams" {
		t.Fatalf("segment 0 = %+v, want bold 'Teams'", segs[0])
	}
	var sawCode, sawContinuation bool
	for _, s := range segs {
		if s.Kind == KindCode && s.Text == "databases" {
			sawCode = true
		}
		if s.Kind == KindText && contains(s.Text, "wrapped continuation") {
			sawContinuation = true
		}
	}
	if !sawCode {
		t.Fatal("expected an inline code segment 'databases'")
	}
	if !sawContinuation {
		t.Fatal("expected the wrapped continuation line to be joined into the item")
	}

	// Link segment in item 2.
	link := added.Items[1].Segments
	var gotLink bool
	for _, s := range link {
		if s.Kind == KindLink && s.URL == "docs/backlog.md" && s.Text == "doc link" {
			gotLink = true
		}
	}
	if !gotLink {
		t.Fatalf("expected a link segment to docs/backlog.md, got %+v", link)
	}

	// Second release has a bare note (no ### group).
	r1 := doc.Releases[1]
	if len(r1.Notes) != 1 || len(r1.Groups) != 0 {
		t.Fatalf("release 1 = %d notes / %d groups, want 1 note / 0 groups", len(r1.Notes), len(r1.Groups))
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
