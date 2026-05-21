package templates

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"golang.org/x/net/html"
)

// TestAccessibility renders representative pages and asserts the structural
// WCAG 2.2 AA basics by parsing the HTML — no browser required. It is a
// floor, not a substitute for a full axe-core audit (see docs/backlog.md).
func TestAccessibility(t *testing.T) {
	pages := map[string]templ.Component{
		"login":         Login("", ""),
		"signup":        Signup("", ""),
		"signup-closed": SignupClosed(),
		"legal-cookies": LegalPage(LayoutCtx{}, "Cookie Policy", LegalCookies()),
		"legal-subproc": LegalPage(LayoutCtx{}, "Sub-processors", LegalSubprocessors()),
		"help":          HelpPage(LayoutCtx{}),
		"admin-home":    AdminHome(LayoutCtx{}),
	}

	for name, comp := range pages {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := comp.Render(context.Background(), &buf); err != nil {
				t.Fatalf("render: %v", err)
			}
			doc, err := html.Parse(&buf)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			checkAccessibility(t, doc)
		})
	}
}

func checkAccessibility(t *testing.T, doc *html.Node) {
	t.Helper()
	a := &a11yScan{labelledIDs: map[string]bool{}}
	a.walk(doc, false)

	// 3.1.1 — the document declares a language.
	if a.htmlLang == "" {
		t.Error("WCAG 3.1.1: <html> is missing a non-empty lang attribute")
	}
	// 2.4.1 — a skip link / bypass mechanism exists.
	if !a.hasSkipLink {
		t.Error("WCAG 2.4.1: no skip link (an <a href=\"#...\">) found")
	}
	// 1.3.1 — a main landmark exists.
	if !a.hasMain {
		t.Error("WCAG 1.3.1: no <main> landmark")
	}
	// 2.4.6 / page structure — exactly one top-level heading.
	if a.h1Count == 0 {
		t.Error("WCAG 2.4.6: page has no <h1>")
	}
	if a.h1Count > 1 {
		t.Errorf("page has %d <h1> elements, want exactly 1", a.h1Count)
	}
	// 4.1.2 — every form control has an accessible name.
	for _, c := range a.controls {
		if !c.named(a.labelledIDs) {
			t.Errorf("WCAG 4.1.2: <%s%s> has no accessible name "+
				"(needs a <label for>, aria-label, or aria-labelledby)",
				c.tag, attrHint(c))
		}
	}
	// 4.1.2 / 2.4.4 — every link and button has a discernible name.
	for _, name := range a.unnamedInteractive {
		t.Errorf("WCAG 4.1.2: <%s> has no discernible text or aria-label", name)
	}
}

type control struct {
	tag            string // input | select | textarea
	id             string
	typ            string
	ariaLabel      bool
	ariaLabelledby bool
	insideLabel    bool
}

func (c control) named(labelledIDs map[string]bool) bool {
	return c.ariaLabel || c.ariaLabelledby || c.insideLabel ||
		(c.id != "" && labelledIDs[c.id])
}

func attrHint(c control) string {
	if c.id != "" {
		return " id=" + c.id
	}
	if c.typ != "" {
		return " type=" + c.typ
	}
	return ""
}

type a11yScan struct {
	htmlLang           string
	hasSkipLink        bool
	hasMain            bool
	h1Count            int
	labelledIDs        map[string]bool
	controls           []control
	unnamedInteractive []string
}

func (a *a11yScan) walk(n *html.Node, insideLabel bool) {
	if n.Type == html.ElementNode {
		switch n.Data {
		case "html":
			a.htmlLang = attr(n, "lang")
		case "main":
			a.hasMain = true
		case "h1":
			a.h1Count++
		case "label":
			if id := attr(n, "for"); id != "" {
				a.labelledIDs[id] = true
			}
			insideLabel = true
		case "a":
			if strings.HasPrefix(attr(n, "href"), "#") {
				a.hasSkipLink = true
			}
			if accessibleText(n) == "" {
				a.unnamedInteractive = append(a.unnamedInteractive, "a")
			}
		case "button":
			if accessibleText(n) == "" {
				a.unnamedInteractive = append(a.unnamedInteractive, "button")
			}
		case "input", "select", "textarea":
			typ := strings.ToLower(attr(n, "type"))
			// Hidden and button-like inputs need no label.
			if n.Data == "input" && (typ == "hidden" || typ == "submit" ||
				typ == "button" || typ == "reset") {
				break
			}
			a.controls = append(a.controls, control{
				tag:            n.Data,
				id:             attr(n, "id"),
				typ:            typ,
				ariaLabel:      attr(n, "aria-label") != "",
				ariaLabelledby: attr(n, "aria-labelledby") != "",
				insideLabel:    insideLabel,
			})
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		a.walk(c, insideLabel)
	}
}

// accessibleText returns the trimmed text content of a node plus any
// aria-label — the discernible name of a link or button.
func accessibleText(n *html.Node) string {
	if l := attr(n, "aria-label"); l != "" {
		return l
	}
	var b strings.Builder
	var collect func(*html.Node)
	collect = func(node *html.Node) {
		if node.Type == html.TextNode {
			b.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			collect(c)
		}
	}
	collect(n)
	return strings.TrimSpace(b.String())
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
