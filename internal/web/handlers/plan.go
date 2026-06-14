package handlers

import (
	"net/http"

	"github.com/preshotcome/dokaz/internal/account"
	"github.com/preshotcome/dokaz/internal/web/templates"
)

// enforceLimit checks a subscription-tier resource cap. When the account has
// reached the cap it renders the plan-limit page with 403 and returns true —
// the caller must stop. Otherwise it returns false and the create proceeds.
func (h *Handlers) enforceLimit(w http.ResponseWriter, r *http.Request, lc templates.LayoutCtx, resource string, count, limit int) bool {
	if !account.AtLimit(count, limit) {
		return false
	}
	w.WriteHeader(http.StatusForbidden)
	render(w, r, templates.PlanLimit(lc, resource, string(lc.Account.Plan), limit))
	return true
}
