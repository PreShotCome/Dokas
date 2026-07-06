// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

package handlers

import (
	_ "embed"
	"net/http"

	"github.com/preshotcome/dokaz/internal/web/templates"
)

// openAPIDoc is the hand-authored OpenAPI 3.1 document for the /v1 API.
//
//go:embed openapi.json
var openAPIDoc []byte

func (h *Handlers) openAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(openAPIDoc)
}

// docsPage is the human-readable API reference. It links to openapi.json
// for machine consumers.
func (h *Handlers) docsPage(w http.ResponseWriter, r *http.Request) {
	render(w, r, templates.DocsPage(h.layoutCtx(r)))
}
