// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/preshotcome/dokaz/internal/account"
	"github.com/preshotcome/dokaz/internal/audit"
	"github.com/preshotcome/dokaz/internal/auth"
	"github.com/preshotcome/dokaz/internal/drill"
	"github.com/preshotcome/dokaz/internal/web/templates"
)

// databaseScope resolves the current request's database/drill visibility
// (issue #29, teams-within-org). Owner/admin — the roles that can manage teams
// (ActionTeamWrite) — see every database in the account; every other role is
// limited to unassigned databases plus the ones on their teams.
//
// Fail-safe: any inability to establish the caller as privileged or to load
// their team set collapses to the *narrowest* scope (unassigned only). A bug
// or a query error must never widen what a member can see.
func (h *Handlers) databaseScope(r *http.Request, lc templates.LayoutCtx) drill.Scope {
	if lc.Account == nil || lc.User == nil {
		return drill.Scope{}
	}
	return h.databaseScopeCtx(r.Context(), lc.Account.ID, lc.User.ID)
}

// databaseScopeCtx is the context-only form for handlers (e.g. drillCreate)
// that resolve account/user from the request context rather than a LayoutCtx.
func (h *Handlers) databaseScopeCtx(ctx context.Context, accountID, userID uuid.UUID) drill.Scope {
	m, ok := auth.MembershipFromContext(ctx)
	if !ok {
		return drill.Scope{}
	}
	if auth.Allowed(m.Role, auth.ActionTeamWrite) {
		return drill.ScopeAll()
	}
	ids, err := h.accounts.TeamIDsForUser(ctx, accountID, userID)
	if err != nil {
		h.logger().Error("teams: scope lookup failed",
			"account_id", accountID, "user_id", userID, "err", err)
		return drill.Scope{}
	}
	return drill.Scope{TeamIDs: ids}
}

// teamsPage renders the team-management page. Gated on ActionTeamRead by the
// route; the write controls render only for ActionTeamWrite (owner/admin).
func (h *Handlers) teamsPage(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	view, err := h.buildTeamsView(r, lc)
	if err != nil {
		http.Error(w, "load teams: "+err.Error(), http.StatusInternalServerError)
		return
	}
	render(w, r, templates.TeamsPage(lc, view, ""))
}

// buildTeamsView assembles the teams, their members, and their assigned
// databases in a shape the template renders directly.
func (h *Handlers) buildTeamsView(r *http.Request, lc templates.LayoutCtx) (templates.TeamsView, error) {
	teams, err := h.accounts.ListTeams(r.Context(), lc.Account.ID)
	if err != nil {
		return templates.TeamsView{}, err
	}
	members, err := h.accounts.ListMembers(r.Context(), lc.Account.ID)
	if err != nil {
		return templates.TeamsView{}, err
	}
	edges, err := h.accounts.TeamMemberships(r.Context(), lc.Account.ID)
	if err != nil {
		return templates.TeamsView{}, err
	}
	// Owner/admin viewing this page: list every database so they can assign
	// any of them. team_id lives on the Target.
	targets, err := h.drills.ListTargets(r.Context(), lc.Account.ID, drill.ScopeAll())
	if err != nil {
		return templates.TeamsView{}, err
	}

	emailByUser := make(map[uuid.UUID]string, len(members))
	for _, m := range members {
		emailByUser[m.UserID] = m.Email
	}
	membersByTeam := make(map[uuid.UUID][]templates.TeamMemberView)
	for _, e := range edges {
		membersByTeam[e.TeamID] = append(membersByTeam[e.TeamID], templates.TeamMemberView{
			UserID: e.UserID, Email: emailByUser[e.UserID],
		})
	}
	dbsByTeam := make(map[uuid.UUID][]templates.TeamDatabaseView)
	var unassigned []templates.TeamDatabaseView
	for _, t := range targets {
		dv := templates.TeamDatabaseView{ID: t.ID, Name: t.Name}
		if t.TeamID == nil {
			unassigned = append(unassigned, dv)
		} else {
			dbsByTeam[*t.TeamID] = append(dbsByTeam[*t.TeamID], dv)
		}
	}

	view := templates.TeamsView{
		CanManage:  auth.Allowed(lc.Membership.Role, auth.ActionTeamWrite),
		Members:    members,
		Unassigned: unassigned,
	}
	for _, tm := range teams {
		view.Teams = append(view.Teams, templates.TeamView{
			ID:        tm.ID,
			Name:      tm.Name,
			Members:   membersByTeam[tm.ID],
			Databases: dbsByTeam[tm.ID],
		})
	}
	return view, nil
}

func (h *Handlers) teamCreate(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := trimName(r.PostFormValue("name"))
	if name == "" {
		h.renderTeamsError(w, r, lc, "Team name is required.")
		return
	}
	t, err := h.accounts.CreateTeam(r.Context(), lc.Account.ID, name)
	if err == account.ErrTeamNameTaken {
		h.renderTeamsError(w, r, lc, "A team with that name already exists.")
		return
	}
	if err != nil {
		http.Error(w, "create team: "+err.Error(), http.StatusInternalServerError)
		return
	}
	h.auditTeam(r, lc, "team.created", t.ID, map[string]any{"name": name})
	http.Redirect(w, r, "/account/teams", http.StatusSeeOther)
}

func (h *Handlers) teamRename(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	teamID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := trimName(r.PostFormValue("name"))
	if name == "" {
		h.renderTeamsError(w, r, lc, "Team name is required.")
		return
	}
	err = h.accounts.RenameTeam(r.Context(), lc.Account.ID, teamID, name)
	if err == account.ErrTeamNameTaken {
		h.renderTeamsError(w, r, lc, "A team with that name already exists.")
		return
	}
	if err == account.ErrNotFound {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "rename team: "+err.Error(), http.StatusInternalServerError)
		return
	}
	h.auditTeam(r, lc, "team.renamed", teamID, map[string]any{"name": name})
	http.Redirect(w, r, "/account/teams", http.StatusSeeOther)
}

func (h *Handlers) teamDelete(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	teamID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := h.accounts.DeleteTeam(r.Context(), lc.Account.ID, teamID); err != nil && err != account.ErrNotFound {
		http.Error(w, "delete team: "+err.Error(), http.StatusInternalServerError)
		return
	}
	h.auditTeam(r, lc, "team.deleted", teamID, nil)
	http.Redirect(w, r, "/account/teams", http.StatusSeeOther)
}

func (h *Handlers) teamMemberAdd(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	teamID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	userID, err := uuid.Parse(r.PostFormValue("user_id"))
	if err != nil {
		h.renderTeamsError(w, r, lc, "Pick a member to add.")
		return
	}
	if err := h.accounts.AddTeamMember(r.Context(), lc.Account.ID, teamID, userID); err == account.ErrNotFound {
		http.NotFound(w, r)
		return
	} else if err != nil {
		http.Error(w, "add member: "+err.Error(), http.StatusInternalServerError)
		return
	}
	h.auditTeam(r, lc, "team.member_added", teamID, map[string]any{"user_id": userID.String()})
	http.Redirect(w, r, "/account/teams", http.StatusSeeOther)
}

func (h *Handlers) teamMemberRemove(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	teamID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "user_id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := h.accounts.RemoveTeamMember(r.Context(), lc.Account.ID, teamID, userID); err != nil {
		http.Error(w, "remove member: "+err.Error(), http.StatusInternalServerError)
		return
	}
	h.auditTeam(r, lc, "team.member_removed", teamID, map[string]any{"user_id": userID.String()})
	http.Redirect(w, r, "/account/teams", http.StatusSeeOther)
}

// teamDatabaseAssign moves a database onto a team, or unassigns it when the
// team_id form field is empty. The database and team are both account-scoped
// in the store, so a forged id from another tenant 404s.
func (h *Handlers) teamDatabaseAssign(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	targetID, err := uuid.Parse(r.PostFormValue("database_id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var teamID *uuid.UUID
	if raw := r.PostFormValue("team_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		teamID = &id
	}
	if err := h.drills.SetTargetTeam(r.Context(), lc.Account.ID, targetID, teamID); err == drill.ErrNotFound {
		http.NotFound(w, r)
		return
	} else if err != nil {
		http.Error(w, "assign database: "+err.Error(), http.StatusInternalServerError)
		return
	}
	meta := map[string]any{"database_id": targetID.String()}
	if teamID != nil {
		meta["team_id"] = teamID.String()
	}
	h.auditTeam(r, lc, "team.database_assigned", targetID, meta)
	http.Redirect(w, r, "/account/teams", http.StatusSeeOther)
}

func (h *Handlers) renderTeamsError(w http.ResponseWriter, r *http.Request, lc templates.LayoutCtx, msg string) {
	view, err := h.buildTeamsView(r, lc)
	if err != nil {
		http.Error(w, "load teams: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusBadRequest)
	render(w, r, templates.TeamsPage(lc, view, msg))
}

func (h *Handlers) auditTeam(r *http.Request, lc templates.LayoutCtx, action string, teamID uuid.UUID, meta map[string]any) {
	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: &lc.Account.ID, ActorID: &lc.User.ID, Action: action,
		TargetKind: "team", TargetID: teamID.String(),
		IP: audit.ClientIP(r), UserAgent: r.UserAgent(), Metadata: meta,
	})
}

// trimName normalises a submitted name: trims surrounding whitespace and caps
// length so the UI and DB stay tidy.
func trimName(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}
