// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

package handlers

import (
	"net/http"

	dokaz "github.com/preshotcome/dokaz"
	"github.com/preshotcome/dokaz/internal/changelog"
	"github.com/preshotcome/dokaz/internal/web/templates"
)

// changelogDoc is parsed once from the embedded CHANGELOG.md — the content is
// static at build time, so there is no reason to re-parse per request.
var changelogDoc = changelog.Parse(dokaz.ChangelogMarkdown)

// changelogPage renders the public, dated record of shipped changes.
func (h *Handlers) changelogPage(w http.ResponseWriter, r *http.Request) {
	render(w, r, templates.ChangelogPage(h.layoutCtx(r), changelogDoc))
}
