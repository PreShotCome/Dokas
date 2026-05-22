package handlers

import (
	"net/http"

	"github.com/preshotcome/anything/internal/web/templates"
)

// helpPage serves the in-app help / FAQ. It's public — an interim until the
// full Astro docs site (Phase 7) ships.
func (h *Handlers) helpPage(w http.ResponseWriter, r *http.Request) {
	render(w, r, templates.HelpPage(h.layoutCtx(r)))
}

// howItWorks serves the public explainer: what backup drilling is and how
// Soteria does it.
func (h *Handlers) howItWorks(w http.ResponseWriter, r *http.Request) {
	render(w, r, templates.HowItWorks(h.layoutCtx(r)))
}
