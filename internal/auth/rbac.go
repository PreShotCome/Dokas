package auth

import (
	"context"
	"errors"
	"net/http"

	"github.com/preshotcome/anything/internal/account"
)

// Action is the verb-on-resource enum the Authorize matrix is keyed on.
// Each constant names exactly the boundary we check, so a quick grep finds
// every site that gates on a particular action.
type Action string

const (
	ActionAccountRead  Action = "account.read"
	ActionAccountWrite Action = "account.write"

	ActionMemberRead  Action = "member.read"
	ActionMemberWrite Action = "member.write"

	ActionBillingRead  Action = "billing.read"
	ActionBillingWrite Action = "billing.write"

	ActionTargetRead  Action = "target.read"
	ActionTargetWrite Action = "target.write"

	ActionDrillRead  Action = "drill.read"
	ActionDrillWrite Action = "drill.write"

	ActionEvidenceRead Action = "evidence.read"
)

// roleMatrix is the single source of truth for what each role can do. Keep
// every action explicitly listed (no defaults) so the table reviews well.
var roleMatrix = map[account.Role]map[Action]bool{
	account.RoleOwner: {
		ActionAccountRead: true, ActionAccountWrite: true,
		ActionMemberRead: true, ActionMemberWrite: true,
		ActionBillingRead: true, ActionBillingWrite: true,
		ActionTargetRead: true, ActionTargetWrite: true,
		ActionDrillRead: true, ActionDrillWrite: true,
		ActionEvidenceRead: true,
	},
	account.RoleAdmin: {
		ActionAccountRead: true, ActionAccountWrite: true,
		ActionMemberRead: true, ActionMemberWrite: true,
		ActionBillingRead: true, ActionBillingWrite: false,
		ActionTargetRead: true, ActionTargetWrite: true,
		ActionDrillRead: true, ActionDrillWrite: true,
		ActionEvidenceRead: true,
	},
	account.RoleMember: {
		ActionAccountRead: true, ActionAccountWrite: false,
		ActionMemberRead: true, ActionMemberWrite: false,
		ActionBillingRead: true, ActionBillingWrite: false,
		ActionTargetRead: true, ActionTargetWrite: true,
		ActionDrillRead: true, ActionDrillWrite: true,
		ActionEvidenceRead: true,
	},
	account.RoleViewer: {
		ActionAccountRead: true, ActionAccountWrite: false,
		ActionMemberRead: true, ActionMemberWrite: false,
		ActionBillingRead: true, ActionBillingWrite: false,
		ActionTargetRead: true, ActionTargetWrite: false,
		ActionDrillRead: true, ActionDrillWrite: false,
		ActionEvidenceRead: true,
	},
}

// Allowed reports whether the role permits the action. Unknown roles are
// always denied (safe default).
func Allowed(role account.Role, action Action) bool {
	if m, ok := roleMatrix[role]; ok {
		return m[action]
	}
	return false
}

// Authorize checks the current request's membership against an action. The
// membership is whatever LoadCurrentAccount stashed on the context.
//
// Returns nil on allow, ErrForbidden on deny, ErrNoAccount when the request
// has no current account (e.g. user is logged in but hasn't been assigned
// one — shouldn't happen after Phase 3 signup but defended against here).
func Authorize(ctx context.Context, action Action) error {
	m, ok := MembershipFromContext(ctx)
	if !ok {
		return ErrNoAccount
	}
	if !Allowed(m.Role, action) {
		return ErrForbidden
	}
	return nil
}

var (
	ErrNoAccount = errors.New("auth: no current account")
	ErrForbidden = errors.New("auth: forbidden")
)

// RequireAction is a middleware that gates a route on Authorize. It returns
// 403 on deny rather than redirecting, since these are authenticated
// requests where the auth gate already passed.
func RequireAction(action Action) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := Authorize(r.Context(), action); err != nil {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
