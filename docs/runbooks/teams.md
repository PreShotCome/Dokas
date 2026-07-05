# Teams within an org (issue #29)

Sub-partitioning of a single account (org) so a large customer can carve
its databases and people into teams — "Payments", "Analytics" — where a
non-privileged member sees only their teams' databases, not the whole
org's. Owners/admins keep full oversight.

## Model

```
accounts (org / tenant)                       ── existing
 └─ memberships (user_id, role)               ── existing: org-level role
 └─ teams (id, account_id, name)              ── NEW
     └─ team_memberships (team_id, user_id)   ── NEW: user ∈ many teams
 └─ database_targets.team_id  → teams(id)     ── NEW: DB ∈ 0..1 team
     └─ drills (inherit the target's team)    ── scoped via the target
```

- **`teams`** — account-scoped, soft-deletable, name unique per account
  (case-insensitive). `ON DELETE CASCADE` from the account.
- **`team_memberships`** — many-to-many user↔team *within* an account. A
  member can sit on several teams. Rows cascade when either the team or the
  user is deleted. Membership on a team does not imply org membership — the
  handler only ever assigns users who already have an org `membership`.
- **`database_targets.team_id`** — nullable FK, `ON DELETE SET NULL`.
  `NULL` = **unassigned**, meaning account-wide visibility (the pre-teams
  default; every existing database backfills to `NULL`). A database belongs
  to at most one team.

## Visibility rule (the security core)

Resolved once per request into an `account.Scope`:

| Org role                         | Databases visible                                  |
|----------------------------------|----------------------------------------------------|
| owner, admin                     | **all** databases in the account (`Scope.All`)     |
| member, viewer, exec, auditor    | `team_id IS NULL` **or** `team_id ∈ viewer's teams`|

- Privileged roles (owner/admin) manage teams, so they must see everything;
  `Scope.All` skips the team predicate entirely.
- For everyone else the predicate is
  `(team_id IS NULL OR team_id = ANY($teams))`. An empty team set still sees
  the unassigned databases — teams *narrow* nothing that was already shared.
- **Drills** carry no team column; they inherit their target's. The drill
  list gates on `EXISTS (target visible under the same predicate)`, so a
  member never sees drills (or evidence) for a database outside their teams.
- The single-object getters (`GetTarget`, drill fetch) take the same scope,
  so a hand-typed `/databases/{id}` URL for another team's database 404s
  rather than leaking the row.

Unchanged in v1 (documented follow-up, not a regression — these were never
team-scoped): heartbeats, API keys, and webhooks stay account-wide. They
carry no `team_id`; scoping them is a mechanical repeat of this pattern when
a customer asks for it.

## Management surface

`/account/teams` — **owner/admin only** (`ActionTeamWrite`):

- create / rename / delete a team,
- add / remove org members on a team,
- assign / unassign a database to a team.

Deleting a team `SET NULL`s its databases (they fall back to account-wide),
so a delete can never orphan or hide a database. Members see a read-only
"your teams" summary; the full roster + database assignment is admin-only.

## RBAC

Two new actions in `internal/auth/rbac.go`:

- `ActionTeamRead` — every role (see your own team labels).
- `ActionTeamWrite` — owner + admin only (manage teams).

Resource authorization is now two-dimensional: the **role** gate
(`RequireAction`) is unchanged, and the **scope** filter (`account.Scope`)
is applied inside the store queries. A member has `target.write`, but only
for databases in their scope — the handler resolves the target through the
scoped getter first, so the role check and the scope check compose.

## Plan limits

`account.Limits` gains nothing required: teams are an organisational
convenience, not a metered resource, so v1 does not cap team count. If
abuse surfaces, add `Limits.Teams` and gate `CreateTeam` the same way seats
are gated.

## Migration

`20260705000002_teams.sql` — creates the two tables and the nullable column
with a `SET NULL` FK. Down drops the column, then the tables. No backfill:
every existing database stays `team_id IS NULL` (account-wide), so the
feature is inert until an admin creates a team and assigns a database. The
migration round-trip test (`cmd/migrate`) covers up→down→up.
