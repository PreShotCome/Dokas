// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

package handlers

import (
	"net/http"

	"github.com/preshotcome/dokaz/internal/web/templates"
)

// helpPage serves the in-app help / FAQ. It's public — an interim until the
// full Astro docs site (Phase 7) ships.
func (h *Handlers) helpPage(w http.ResponseWriter, r *http.Request) {
	render(w, r, templates.HelpPage(h.layoutCtx(r)))
}

// howItWorks serves the public explainer: what backup drilling is and how
// Dokaz does it.
func (h *Handlers) howItWorks(w http.ResponseWriter, r *http.Request) {
	render(w, r, templates.HowItWorks(h.layoutCtx(r)))
}
