package auth

import (
	"testing"

	"github.com/preshotcome/anything/internal/account"
)

func TestRoleMatrix(t *testing.T) {
	cases := []struct {
		role   account.Role
		action Action
		want   bool
	}{
		// Owner: everything.
		{account.RoleOwner, ActionAccountWrite, true},
		{account.RoleOwner, ActionBillingWrite, true},
		{account.RoleOwner, ActionMemberWrite, true},
		{account.RoleOwner, ActionDrillWrite, true},

		// Admin: everything except billing writes.
		{account.RoleAdmin, ActionAccountWrite, true},
		{account.RoleAdmin, ActionMemberWrite, true},
		{account.RoleAdmin, ActionDrillWrite, true},
		{account.RoleAdmin, ActionBillingWrite, false},

		// Member: can run drills + manage targets, but not members/account.
		{account.RoleMember, ActionDrillWrite, true},
		{account.RoleMember, ActionTargetWrite, true},
		{account.RoleMember, ActionMemberWrite, false},
		{account.RoleMember, ActionAccountWrite, false},
		{account.RoleMember, ActionBillingWrite, false},

		// Viewer: reads only.
		{account.RoleViewer, ActionDrillRead, true},
		{account.RoleViewer, ActionTargetRead, true},
		{account.RoleViewer, ActionEvidenceRead, true},
		{account.RoleViewer, ActionAccountRead, true},
		{account.RoleViewer, ActionDrillWrite, false},
		{account.RoleViewer, ActionTargetWrite, false},
		{account.RoleViewer, ActionMemberWrite, false},

		// Unknown role: deny everything.
		{account.Role("intern"), ActionDrillRead, false},
	}

	for _, c := range cases {
		if got := Allowed(c.role, c.action); got != c.want {
			t.Errorf("Allowed(%s, %s) = %v, want %v", c.role, c.action, got, c.want)
		}
	}
}

func TestEveryRoleCanRead(t *testing.T) {
	// Every known role should at least read drills — the dashboard would be
	// unusable otherwise.
	for _, role := range []account.Role{
		account.RoleOwner, account.RoleAdmin, account.RoleMember, account.RoleViewer,
	} {
		if !Allowed(role, ActionDrillRead) {
			t.Errorf("role %s cannot read drills", role)
		}
	}
}

func TestAuthorizeWithoutMembership(t *testing.T) {
	// A context with no membership must be denied (not panic).
	if err := Authorize(t.Context(), ActionDrillRead); err != ErrNoAccount {
		t.Errorf("Authorize without membership = %v, want ErrNoAccount", err)
	}
}

func TestAuthorizeForbidden(t *testing.T) {
	ctx := WithMembership(t.Context(), &account.Membership{Role: account.RoleViewer})
	if err := Authorize(ctx, ActionDrillWrite); err != ErrForbidden {
		t.Errorf("viewer drill.write = %v, want ErrForbidden", err)
	}
	if err := Authorize(ctx, ActionDrillRead); err != nil {
		t.Errorf("viewer drill.read = %v, want nil", err)
	}
}
