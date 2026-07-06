// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

// Package changelog parses the repository's CHANGELOG.md into a structured
// form so the public /changelog page can render it with the site's own styling
// (rather than shipping a markdown renderer or relying on unavailable prose
// classes). It understands the narrow subset of Markdown the changelog uses:
// "## [date]" releases, "### Category" groups, "- " bullets (with wrapped
// continuation lines), and inline **bold**, `code`, and [text](url).
package changelog

import (
	"regexp"
	"strings"
)

// Segment kinds for inline rendering.
const (
	KindText = "text"
	KindBold = "bold"
	KindCode = "code"
	KindLink = "link"
)

// Segment is one inline run within an item.
type Segment struct {
	Kind string
	Text string
	URL  string // set only for KindLink
}

// Item is a single bullet or note, split into inline segments.
type Item struct {
	Segments []Segment
}

// Group is a "### Category" block (Added / Changed / Fixed / Security).
type Group struct {
	Title string
	Items []Item
}

// Release is one "## [date]" section: category groups plus any bullets that
// sit directly under the release with no category (Notes).
type Release struct {
	Heading string
	Groups  []Group
	Notes   []Item
}

// Doc is the whole changelog, newest release first. The file-level intro and
// "# Changelog" title are intentionally dropped — the page supplies its own.
type Doc struct {
	Releases []Release
}

var inlineRe = regexp.MustCompile("\\*\\*(.+?)\\*\\*|`([^`]+)`|\\[([^\\]]+)\\]\\(([^)]+)\\)")

// parseInline splits a line into text / bold / code / link segments.
func parseInline(s string) []Segment {
	var segs []Segment
	last := 0
	for _, m := range inlineRe.FindAllStringSubmatchIndex(s, -1) {
		if m[0] > last {
			segs = append(segs, Segment{Kind: KindText, Text: s[last:m[0]]})
		}
		switch {
		case m[2] >= 0:
			segs = append(segs, Segment{Kind: KindBold, Text: s[m[2]:m[3]]})
		case m[4] >= 0:
			segs = append(segs, Segment{Kind: KindCode, Text: s[m[4]:m[5]]})
		case m[6] >= 0:
			segs = append(segs, Segment{Kind: KindLink, Text: s[m[6]:m[7]], URL: s[m[8]:m[9]]})
		}
		last = m[1]
	}
	if last < len(s) {
		segs = append(segs, Segment{Kind: KindText, Text: s[last:]})
	}
	return segs
}

// Parse turns CHANGELOG.md into a Doc.
func Parse(md string) Doc {
	var releases []Release
	var cur Release
	var group Group
	var item strings.Builder
	var haveRelease, haveGroup, haveItem bool

	flushItem := func() {
		if !haveItem {
			return
		}
		text := strings.TrimSpace(item.String())
		item.Reset()
		haveItem = false
		if text == "" {
			return
		}
		it := Item{Segments: parseInline(text)}
		switch {
		case haveGroup:
			group.Items = append(group.Items, it)
		case haveRelease:
			cur.Notes = append(cur.Notes, it)
		}
	}
	flushGroup := func() {
		flushItem()
		if haveGroup {
			cur.Groups = append(cur.Groups, group)
			group = Group{}
			haveGroup = false
		}
	}
	flushRelease := func() {
		flushGroup()
		if haveRelease {
			releases = append(releases, cur)
			cur = Release{}
			haveRelease = false
		}
	}

	for _, raw := range strings.Split(md, "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "## "):
			flushRelease()
			cur = Release{Heading: cleanHeading(trimmed[3:])}
			haveRelease = true
		case strings.HasPrefix(trimmed, "### "):
			flushGroup()
			group = Group{Title: strings.TrimSpace(trimmed[4:])}
			haveGroup = true
		case strings.HasPrefix(trimmed, "- "):
			flushItem()
			item.WriteString(strings.TrimSpace(trimmed[2:]))
			haveItem = true
		case trimmed == "":
			flushItem()
		default:
			if haveItem {
				item.WriteString(" ")
				item.WriteString(trimmed)
			}
		}
	}
	flushRelease()
	return Doc{Releases: releases}
}

// cleanHeading strips the [brackets] off a "## [date]" heading.
func cleanHeading(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	return s
}
