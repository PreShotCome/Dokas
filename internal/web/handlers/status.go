// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/preshotcome/dokaz/internal/web/templates"
)

// statusPage is a public, at-a-glance health page for the service. It runs a
// few live dependency checks and renders green/red per component plus an
// overall verdict — the trust signal customers expect from infra SaaS.
func (h *Handlers) statusPage(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	components := []templates.StatusComponent{
		{Name: "Website & API", Detail: "Serving requests", OK: true},
		{Name: "Database", Detail: "Primary datastore", OK: h.pool.Ping(ctx) == nil},
		{Name: "Evidence signing", Detail: "Proof-of-Recovery signatures", OK: h.signer != nil},
	}
	// A degraded check should never be cached as healthy.
	w.Header().Set("Cache-Control", "no-store")
	render(w, r, templates.StatusPage(h.layoutCtx(r), components))
}
