# User Sessions Enhancements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add filters + facets, an MCP auth-tab sessions panel, right-click/⋮ revoke, brand-themed status badges, and two read-only assistant platform tools to the existing User Sessions feature.

**Architecture:** Backend extends the existing `userSessions` service (new `listFacets` method, a `client_id` list filter, an internal `id` list filter) and adds a `platformtools/usersessions` package with two read-only executors registered in the platform-tools registry. Frontend extracts shared `SessionRow`/`SessionStatusBadge` units, adds four facet filters to the page, and embeds a sessions panel in the MCP Authentication tab.

**Tech Stack:** Go, Goa (design-first codegen), sqlc/pgx, platform-tools framework; React + TS dashboard, TanStack Query via `@gram/client/react-query`, Moonshine/shadcn UI.

---

## Background facts (verified — trust these)

- List query `ListUserSessionsByProjectID` (`server/internal/usersessions/queries.sql`) is enriched and filters by `subject_urn`, `user_session_issuer_id`, `status` (CASE; NULL→live-only), `cursor`. Row type `repo.ListUserSessionsByProjectIDRow`; view builder `mv.BuildUserSessionView` / `BuildUserSessionListView`.
- Handler `ListUserSessions` (`server/internal/usersessions/sessionhandlers.go:26-73`): gets project via `contextvalues.GetAuthContext(ctx)`, `Require(authz.ScopeProjectRead)`, builds `repo.ListUserSessionsByProjectIDParams{...}`, `pageLimit(payload.Limit)`, `parseCursor`, `conv.PtrToNullUUID`, `conv.PtrToPGTextEmpty`.
- Audit facets pattern: `auditlogs.listFacets` design (`server/design/auditlogs/design.go:47-67,133-151`), handler (`server/internal/auditapi/impl.go:137-172`), SQL returns `{Value, DisplayName, Count int64}`. FE `FacetSelect` (`client/dashboard/src/components/auditlogs/feed.tsx:70-113`), hook `useAuditLogFacets`, wire site `OrgAuditLogs.tsx:466-479`.
- Platform tools: `core.PlatformToolExecutor` (`Descriptor()` + `Call(ctx, env, payload, wr)`); `core.ToolDescriptor{SourceSlug,HandlerName,Name,Description,InputSchema []byte,Annotations,Managed,...}`; helpers `core.BuildInputSchema[T]()`, `core.DecodeInput(payload,&in)`, `core.EncodeResult(wr,v)`, `core.ReadOnlyAnnotations()`. Registry list + `BuildExecutors` (`server/internal/platformtools/registry.go`); `Dependencies` has `DB *pgxpool.Pool`. The logs tool (`logs/tool_search_logs.go`) ignores `env` and scopes via `ctx`. Registry tools already reach the assistant — no `start.go` change.
- Codegen order: `mise gen:goa-server` → `mise gen:sqlc-server` → `mise gen:sdk`. Local sqlc needs `DB_PORT=57912` and the mise-pinned sqlc binary; after regen `git diff --stat` must show only intended files. Commit with `--no-verify` if the worktree oxfmt hook fails. Frontend: forced `tsc -b --noEmit --force`; baseline ~72 unrelated tsc errors exist — only ensure no NEW feature-file errors.

## File Structure

**Backend:**

- Modify `server/internal/usersessions/queries.sql` — add `client_id`+`id` nargs to list query; add 3 facet queries.
- Modify `server/design/usersessions/design.go` — `client_id` param + `listFacets` method + facet types.
- Modify `server/internal/usersessions/sessionhandlers.go` — thread `client_id`; add `ListFacets` handler.
- Create `server/internal/platformtools/usersessions/tools.go` — two read-only executors.
- Modify `server/internal/platformtools/registry.go` + `types.go` — register tools + name constants.
- Tests: extend `listusersessions_test.go`; add `usersessions_facets_test.go`; add `platformtools/usersessions/tools_test.go`.

**Frontend:**

- Modify `client/dashboard/src/lib/user-session-status.ts` — single status→presentation map.
- Create `client/dashboard/src/components/sessions/SessionStatusBadge.tsx`.
- Create `client/dashboard/src/components/sessions/SessionRow.tsx` (context menu + ⋮ + revoke dialog).
- Modify `pages/connect/UserSessions.tsx` — use shared row + 4 facet filters.
- Modify `components/project/UserSessionsCard.tsx` — use shared badge/helper.
- Create `pages/mcp/x/tabs/settings/sections/authentication/McpServerSessionsPanel.tsx`; modify `AuthenticationSection.tsx`.

---

## WORKSTREAM 1 — BACKEND

### Task 1: Extend SQL (client_id + id filters, 3 facet queries)

**Files:** Modify `server/internal/usersessions/queries.sql`; regenerated `repo/queries.sql.go`.

- [ ] **Step 1: Add `client_id` and `id` predicates to `ListUserSessionsByProjectID`**

In `ListUserSessionsByProjectID`, add these two lines into the WHERE clause, immediately after the existing `user_session_issuer_id` predicate:

```sql
  AND (sqlc.narg('client_id')::uuid IS NULL OR s.user_session_client_id = sqlc.narg('client_id')::uuid)
  AND (sqlc.narg('id')::uuid IS NULL OR s.id = sqlc.narg('id')::uuid)
```

- [ ] **Step 2: Append the three facet queries** at the end of `queries.sql`:

```sql
-- name: ListUserSessionServerFacets :many
SELECT s.user_session_issuer_id::text AS value, iss.slug AS display_name, COUNT(*)::bigint AS count
FROM user_sessions AS s
JOIN user_session_issuers AS iss ON iss.id = s.user_session_issuer_id
WHERE iss.project_id = @project_id AND iss.deleted IS FALSE
GROUP BY s.user_session_issuer_id, iss.slug
ORDER BY count DESC, iss.slug ASC;

-- name: ListUserSessionClientFacets :many
SELECT c.id::text AS value, c.client_name AS display_name, COUNT(*)::bigint AS count
FROM user_sessions AS s
JOIN user_session_issuers AS iss ON iss.id = s.user_session_issuer_id
JOIN user_session_clients AS c ON c.id = s.user_session_client_id
WHERE iss.project_id = @project_id AND iss.deleted IS FALSE
GROUP BY c.id, c.client_name
ORDER BY count DESC, c.client_name ASC;

-- name: ListUserSessionUserFacets :many
SELECT s.subject_urn::text AS value,
       COALESCE(u.display_name, u.email, s.subject_urn::text) AS display_name,
       COUNT(*)::bigint AS count
FROM user_sessions AS s
JOIN user_session_issuers AS iss ON iss.id = s.user_session_issuer_id
LEFT JOIN users AS u ON u.id = split_part(s.subject_urn::text, ':', 2)
WHERE iss.project_id = @project_id AND iss.deleted IS FALSE
  AND s.subject_urn::text LIKE 'user:%'
GROUP BY s.subject_urn, u.display_name, u.email
ORDER BY count DESC, display_name ASC;
```

- [ ] **Step 3: Regenerate sqlc**

Run: `DB_PORT=57912 mise gen:sqlc-server` (if the mise task fails on env, run the mise-pinned sqlc binary directly, as done in the base feature).
Verify: `grep -n "ClientID\|ListUserSessionServerFacetsRow\|ListUserSessionClientFacetsRow\|ListUserSessionUserFacetsRow" server/internal/usersessions/repo/queries.sql.go`
Expected: `ListUserSessionsByProjectIDParams` gains `ClientID uuid.NullUUID` and `ID uuid.NullUUID`; three facet Row structs each `{Value string; DisplayName string; Count int64}`. If any `DisplayName` typed as `interface{}`, STOP and report (the COALESCE/non-null-column should yield `string`).
Also `git diff --stat` must show only `queries.sql` + `repo/queries.sql.go`.

- [ ] **Step 4: Confirm build**

Run: `cd server && go build ./internal/usersessions/...`
Expected: builds (existing call site still compiles; new params default to zero/NULL).

- [ ] **Step 5: Commit**

```bash
git add server/internal/usersessions/queries.sql server/internal/usersessions/repo/queries.sql.go
git commit -m "feat(server): add client_id/id filters and facet queries for user sessions"
```

### Task 2: Goa design — client_id param + listFacets method

**Files:** Modify `server/design/usersessions/design.go`; regenerated `server/gen/**`.

- [ ] **Step 1: Add `client_id` to the `listUserSessions` payload + HTTP**

In `Method("listUserSessions", ...)` `Payload`, after the `status` attribute add:

```go
		Attribute("client_id", String, "Filter by the connecting client id.", func() {
			Format(FormatUUID)
		})
```

In its `HTTP` block, after `Param("status")` add:

```go
		Param("client_id")
```

- [ ] **Step 2: Add the `listFacets` method** inside the `Service("userSessions", ...)` block, after the `listUserSessions` method:

```go
	Method("listFacets", func() {
		Description("List available user session facet values (clients, users, servers) in the caller's project.")
		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(ListUserSessionFacetsResult)
		HTTP(func() {
			GET("/rpc/userSessions.listFacets")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "listUserSessionFacets")
		Meta("openapi:extension:x-speakeasy-name-override", "listFacets")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UserSessionFacets"}`)
	})
```

- [ ] **Step 3: Add the facet types** at the end of the file (after `ListUserSessionsResult`):

```go
var UserSessionFacetOption = Type("UserSessionFacetOption", func() {
	Attribute("value", String, "The facet value used for filtering.")
	Attribute("display_name", String, "The label shown for the facet value.")
	Attribute("count", Int64, "Number of sessions for this facet value.")
	Required("value", "display_name", "count")
})

var ListUserSessionFacetsResult = Type("ListUserSessionFacetsResult", func() {
	Attribute("clients", ArrayOf(UserSessionFacetOption), "Connecting client facets.")
	Attribute("users", ArrayOf(UserSessionFacetOption), "Subject (user) facets.")
	Attribute("servers", ArrayOf(UserSessionFacetOption), "Issuer/server facets.")
	Required("clients", "users", "servers")
})
```

- [ ] **Step 4: Regenerate Goa**

Run: `mise gen:goa-server`
Verify: `grep -rn "ClientID\|ListFacetsPayload\|ListUserSessionFacetsResult\|UserSessionFacetOption" server/gen/user_sessions/`
Expected: `ListUserSessionsPayload` gains `ClientID *string`; a `ListFacetsPayload` and `ListUserSessionFacetsResult` (+ `UserSessionFacetOption`) type exist; service interface has a `ListFacets` method.

- [ ] **Step 5: Commit**

```bash
git add server/design/usersessions/design.go server/gen
git commit -m "feat(server): add client_id param and listFacets to userSessions design"
```

### Task 3: Handlers — thread client_id + implement ListFacets (TDD)

**Files:** Modify `server/internal/usersessions/sessionhandlers.go`; Test `server/internal/usersessions/usersessions_facets_test.go` (create), extend `listusersessions_test.go`.

- [ ] **Step 1: Write failing tests**

Create `server/internal/usersessions/usersessions_facets_test.go`:

```go
package usersessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	issuersgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	gen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestListFacets_ServersAndUsers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		Slug: "facets-issuer", AuthnChallengeMode: "chain", SessionDurationHours: 24,
	})
	require.NoError(t, err)
	iid := uuid.MustParse(issuer.ID)

	_, err = seedUserSession(t, ctx, ti.conn, iid, urn.NewUserSubject("alice"))
	require.NoError(t, err)
	_, err = seedUserSession(t, ctx, ti.conn, iid, urn.NewUserSubject("alice"))
	require.NoError(t, err)
	_, err = seedUserSession(t, ctx, ti.conn, iid, urn.NewUserSubject("bob"))
	require.NoError(t, err)

	got, err := ti.service.ListFacets(ctx, &gen.ListFacetsPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	// One server facet for our issuer, count 3.
	require.Len(t, got.Servers, 1)
	require.Equal(t, issuer.ID, got.Servers[0].Value)
	require.Equal(t, "facets-issuer", got.Servers[0].DisplayName)
	require.Equal(t, int64(3), got.Servers[0].Count)

	// Two user facets: alice (2), bob (1), ordered by count desc.
	require.Len(t, got.Users, 2)
	require.Equal(t, "user:alice", got.Users[0].Value)
	require.Equal(t, int64(2), got.Users[0].Count)
	require.Equal(t, "user:bob", got.Users[1].Value)
}
```

Append to `listusersessions_test.go`:

```go
func TestListUserSessions_FilterByClientID_NoMatch(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		Slug: "client-filter-issuer", AuthnChallengeMode: "chain", SessionDurationHours: 24,
	})
	require.NoError(t, err)
	_, err = seedUserSession(t, ctx, ti.conn, uuid.MustParse(issuer.ID), urn.NewUserSubject("x"))
	require.NoError(t, err)

	// Seeded sessions have no client; filtering by a random client id yields none.
	cid := uuid.NewString()
	got, err := ti.service.ListUserSessions(ctx, &gen.ListUserSessionsPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		SubjectUrn: nil, UserSessionIssuerID: nil, Cursor: nil, Limit: nil, Status: nil, ClientID: &cid,
	})
	require.NoError(t, err)
	require.Len(t, got.Items, 0)
}
```

Note: confirm the exact `gen.ListFacetsPayload` field list (it has session/apikey/project fields like other payloads) and that `seedUserSession` seeds sessions with `user_session_client_id` NULL; adjust if it sets a client.

- [ ] **Step 2: Run, verify FAIL**

Run: `cd server && DB_PORT=57912 go test ./internal/usersessions/ -run 'TestListFacets_ServersAndUsers|TestListUserSessions_FilterByClientID_NoMatch' -v`
Expected: compile error (`ListFacets` not implemented / `ClientID` not threaded).

- [ ] **Step 3: Thread `client_id` into `ListUserSessions`**

In `sessionhandlers.go`, in `ListUserSessions`, before building params add:

```go
	clientFilter, err := conv.PtrToNullUUID(payload.ClientID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid client_id").Log(ctx, s.logger)
	}
```

and add `ClientID: clientFilter,` to the `repo.ListUserSessionsByProjectIDParams{...}` literal.

- [ ] **Step 4: Implement `ListFacets`**

Add to `sessionhandlers.go`:

```go
func (s *Service) ListFacets(ctx context.Context, _ *gen.ListFacetsPayload) (*gen.ListUserSessionFacetsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	q := repo.New(s.db)
	clients, err := q.ListUserSessionClientFacets(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list client facets").Log(ctx, s.logger)
	}
	users, err := q.ListUserSessionUserFacets(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list user facets").Log(ctx, s.logger)
	}
	servers, err := q.ListUserSessionServerFacets(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list server facets").Log(ctx, s.logger)
	}

	return &gen.ListUserSessionFacetsResult{
		Clients: clientFacets(clients),
		Users:   userFacets(users),
		Servers: serverFacets(servers),
	}, nil
}

func clientFacets(rows []repo.ListUserSessionClientFacetsRow) []*types.UserSessionFacetOption {
	out := make([]*types.UserSessionFacetOption, len(rows))
	for i, r := range rows {
		out[i] = &types.UserSessionFacetOption{Value: r.Value, DisplayName: r.DisplayName, Count: r.Count}
	}
	return out
}

func userFacets(rows []repo.ListUserSessionUserFacetsRow) []*types.UserSessionFacetOption {
	out := make([]*types.UserSessionFacetOption, len(rows))
	for i, r := range rows {
		out[i] = &types.UserSessionFacetOption{Value: r.Value, DisplayName: r.DisplayName, Count: r.Count}
	}
	return out
}

func serverFacets(rows []repo.ListUserSessionServerFacetsRow) []*types.UserSessionFacetOption {
	out := make([]*types.UserSessionFacetOption, len(rows))
	for i, r := range rows {
		out[i] = &types.UserSessionFacetOption{Value: r.Value, DisplayName: r.DisplayName, Count: r.Count}
	}
	return out
}
```

Confirm `types` is the imported gen types package alias already used in this file (it is — `mv.BuildUserSessionView` returns `*types.UserSession`). If the value field on the generated facet row is `Value string` but typed differently, adjust.

- [ ] **Step 5: Run, verify PASS**

Run: `cd server && DB_PORT=57912 go test ./internal/usersessions/ -run 'TestListFacets|TestListUserSessions' -v`
Expected: PASS (incl. existing tests).

- [ ] **Step 6: Build server**

Run: `cd server && go build ./...`
Expected: success.

- [ ] **Step 7: Commit**

```bash
git add server/internal/usersessions/sessionhandlers.go server/internal/usersessions/usersessions_facets_test.go server/internal/usersessions/listusersessions_test.go
git commit -m "feat(server): client_id filter + listFacets handler for user sessions"
```

### Task 4: Read-only platform tools (TDD)

**Files:** Create `server/internal/platformtools/usersessions/tools.go`; Modify `registry.go`, `types.go`; Test `server/internal/platformtools/usersessions/tools_test.go`.

- [ ] **Step 1: Add tool-name constants** to `server/internal/platformtools/types.go` (in the existing const block):

```go
	ToolNameListUserSessions = "platform_list_user_sessions"
	ToolNameGetUserSession   = "platform_get_user_session"
```

- [ ] **Step 2: Write the tools** — create `server/internal/platformtools/usersessions/tools.go`:

```go
package usersessions

import (
	"context"
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

const maxLimit = 100

type listInput struct {
	Status              string `json:"status,omitempty" jsonschema:"Filter by status: active, expired, revoked, or all. Defaults to live sessions."`
	UserSessionIssuerID string `json:"user_session_issuer_id,omitempty" jsonschema:"Filter to one issuer/server id (UUID)."`
	SubjectURN          string `json:"subject_urn,omitempty" jsonschema:"Exact subject URN to filter by (e.g. user:<id>)."`
	ClientID            string `json:"client_id,omitempty" jsonschema:"Filter to one connecting client id (UUID)."`
	Cursor              string `json:"cursor,omitempty" jsonschema:"Pagination cursor: id of the last item from the previous page."`
	Limit               int    `json:"limit,omitempty" jsonschema:"Page size (default 50, max 100)."`
}

type getInput struct {
	ID string `json:"id" jsonschema:"The user session id (UUID)."`
}

type listResult struct {
	Items      []*types.UserSession `json:"items"`
	NextCursor *string              `json:"next_cursor,omitempty"`
}

func projectID(ctx context.Context) (uuid.UUID, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return uuid.Nil, oops.C(oops.CodeUnauthorized)
	}
	return *authCtx.ProjectID, nil
}

func nullUUID(s string) (uuid.NullUUID, error) {
	if s == "" {
		return uuid.NullUUID{}, nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.NullUUID{}, err
	}
	return uuid.NullUUID{UUID: id, Valid: true}, nil
}

// ---- list ----

type ListTool struct{ db *pgxpool.Pool }

func NewListUserSessionsTool(db *pgxpool.Pool) core.PlatformToolExecutor { return &ListTool{db: db} }

func (t *ListTool) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "user-sessions",
		HandlerName: "list",
		Name:        "platform_list_user_sessions",
		Description: "List user sessions (clients connected into this project's MCP toolsets) with optional filters. Read-only.",
		InputSchema: core.BuildInputSchema[listInput](),
		Variables:   nil,
		Annotations: core.ReadOnlyAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (t *ListTool) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	pid, err := projectID(ctx)
	if err != nil {
		return err
	}
	var in listInput
	if err := core.DecodeInput(payload, &in); err != nil {
		return err
	}
	limit := int32(50)
	if in.Limit > 0 {
		if in.Limit > maxLimit {
			in.Limit = maxLimit
		}
		limit = int32(in.Limit)
	}
	issuer, err := nullUUID(in.UserSessionIssuerID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer_id")
	}
	client, err := nullUUID(in.ClientID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid client_id")
	}
	cursor, err := nullUUID(in.Cursor)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid cursor")
	}
	var subject pgtype.Text
	if in.SubjectURN != "" {
		subject = pgtype.Text{String: in.SubjectURN, Valid: true}
	}
	var status pgtype.Text
	if in.Status != "" {
		status = pgtype.Text{String: in.Status, Valid: true}
	}

	rows, err := repo.New(t.db).ListUserSessionsByProjectID(ctx, repo.ListUserSessionsByProjectIDParams{
		ProjectID:           pid,
		SubjectUrn:          subject,
		UserSessionIssuerID: issuer,
		ClientID:            client,
		Status:              status,
		Cursor:              cursor,
		LimitValue:          limit,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list user sessions")
	}
	var next *string
	if len(rows) >= int(limit) {
		c := rows[len(rows)-1].ID.String()
		next = &c
	}
	return core.EncodeResult(wr, listResult{Items: mv.BuildUserSessionListView(rows), NextCursor: next})
}

// ---- get ----

type GetTool struct{ db *pgxpool.Pool }

func NewGetUserSessionTool(db *pgxpool.Pool) core.PlatformToolExecutor { return &GetTool{db: db} }

func (t *GetTool) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "user-sessions",
		HandlerName: "get",
		Name:        "platform_get_user_session",
		Description: "Get a single user session by id (read-only).",
		InputSchema: core.BuildInputSchema[getInput](),
		Variables:   nil,
		Annotations: core.ReadOnlyAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (t *GetTool) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	pid, err := projectID(ctx)
	if err != nil {
		return err
	}
	var in getInput
	if err := core.DecodeInput(payload, &in); err != nil {
		return err
	}
	id, err := nullUUID(in.ID)
	if err != nil || !id.Valid {
		return oops.E(oops.CodeBadRequest, err, "invalid id")
	}
	rows, err := repo.New(t.db).ListUserSessionsByProjectID(ctx, repo.ListUserSessionsByProjectIDParams{
		ProjectID:  pid,
		ID:         id,
		Status:     pgtype.Text{String: "all", Valid: true},
		LimitValue: 1,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "user session not found")
		}
		return oops.E(oops.CodeUnexpected, err, "get user session")
	}
	if len(rows) == 0 {
		return oops.E(oops.CodeNotFound, nil, "user session not found")
	}
	return core.EncodeResult(wr, mv.BuildUserSessionView(rows[0]))
}

var _ = conv.PtrToPGTextEmpty // keep conv import if unused elsewhere; remove if linter flags
```

Note: remove the trailing `var _ = conv.PtrToPGTextEmpty` line and the `conv` import if `conv` ends up unused (it's only there as a convenience). Confirm `core.BuildInputSchema`, `core.DecodeInput`, `core.EncodeResult`, `core.ReadOnlyAnnotations` signatures match `logs/tool_search_logs.go` usage; adjust calls to match exactly.

- [ ] **Step 3: Register in the registry** — in `server/internal/platformtools/registry.go`, add to the factory list (near the `logs.NewSearchLogsTool` entry):

```go
	func(deps Dependencies) core.PlatformToolExecutor {
		return usersessions.NewListUserSessionsTool(deps.DB)
	},
	func(deps Dependencies) core.PlatformToolExecutor {
		return usersessions.NewGetUserSessionTool(deps.DB)
	},
```

Add the import `usersessions "github.com/speakeasy-api/gram/server/internal/platformtools/usersessions"`. (Confirm the factory return type name used in this file — it may be the local alias `PlatformToolExecutor`; match the surrounding entries exactly.)

- [ ] **Step 4: Write a test** — create `server/internal/platformtools/usersessions/tools_test.go`:

```go
package usersessions

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDescriptors(t *testing.T) {
	t.Parallel()

	list := NewListUserSessionsTool(nil).Descriptor()
	require.Equal(t, "platform_list_user_sessions", list.Name)
	require.NotEmpty(t, list.InputSchema)
	require.NotNil(t, list.Annotations)

	get := NewGetUserSessionTool(nil).Descriptor()
	require.Equal(t, "platform_get_user_session", get.Name)
	require.NotEmpty(t, get.InputSchema)
}
```

(Descriptor() must not touch the DB, so `nil` is safe. A full `Call` integration test would need a DB harness; the descriptor test plus the management-handler tests in Task 3 cover the query path. If a platform-tools DB test harness exists in a sibling package, add a `Call` test mirroring it.)

- [ ] **Step 5: Build + test**

Run: `cd server && go build ./... && go test ./internal/platformtools/usersessions/ -v`
Expected: build clean; `TestDescriptors` passes.

- [ ] **Step 6: Commit**

```bash
git add server/internal/platformtools/usersessions/ server/internal/platformtools/registry.go server/internal/platformtools/types.go
git commit -m "feat(server): read-only user session platform tools for the assistant"
```

### Task 5: Regenerate + build the SDK

**Files:** Regenerated `client/sdk/**`.

- [ ] **Step 1:** Run `mise gen:sdk`.
- [ ] **Step 2:** Verify: `grep -rln "listFacets\|UserSessionFacet\|clientId" client/sdk/src/react-query client/sdk/src/models` — expect a `useUserSessionFacets` hook, a `ListUserSessionFacetsResult`/`UserSessionFacetOption` model, and `clientId` on the list request.
- [ ] **Step 3:** Build: `cd client/sdk && pnpm build` (retry with `NODE_OPTIONS=--max-old-space-size=8192 pnpm build` if needed).
- [ ] **Step 4:** Commit:

```bash
git add client/sdk
git commit -m "chore(sdk): regenerate for user session facets + client_id filter"
```

---

## WORKSTREAM 2 — SHARED FRONTEND MODULE

### Task 6: Status presentation helper + SessionStatusBadge

**Files:** Modify `client/dashboard/src/lib/user-session-status.ts`; Create `client/dashboard/src/components/sessions/SessionStatusBadge.tsx`.

- [ ] **Step 1: Extend the helper** — in `lib/user-session-status.ts`, add below the existing exports:

```ts
import type { ComponentProps } from "react";
import type { Badge } from "@/components/ui/badge";

type BadgeVariant = ComponentProps<typeof Badge>["variant"];

export const STATUS_PRESENTATION: Record<
  SessionStatus,
  { label: string; badgeVariant: BadgeVariant; dotClass: string }
> = {
  active: {
    label: "Active",
    badgeVariant: "default",
    dotClass: "bg-emerald-500",
  },
  expired: {
    label: "Expired",
    badgeVariant: "secondary",
    dotClass: "bg-muted-foreground",
  },
  revoked: {
    label: "Revoked",
    badgeVariant: "destructive",
    dotClass: "bg-destructive",
  },
};
```

Confirm the `Badge` import path/variant type compiles; if `Badge` isn't a forwardRef typed for `ComponentProps`, instead type `BadgeVariant` as `"default" | "secondary" | "destructive"` literally.

- [ ] **Step 2: Create the badge** — `components/sessions/SessionStatusBadge.tsx`:

```tsx
import { Badge } from "@/components/ui/badge";
import type { UserSession } from "@gram/client/models/components";
import { sessionStatus, STATUS_PRESENTATION } from "@/lib/user-session-status";

export function SessionStatusBadge({
  session,
}: {
  session: UserSession;
}): JSX.Element {
  const p = STATUS_PRESENTATION[sessionStatus(session)];
  return <Badge variant={p.badgeVariant}>{p.label}</Badge>;
}
```

If `Badge` uses a compound API (`Badge.Text`), wrap the label accordingly — match an existing `Badge` usage (e.g. in `MCPDetails.tsx`).

- [ ] **Step 3: Type-check** `cd client/dashboard && ./node_modules/.bin/tsc -b --noEmit --force 2>&1 | grep -E "SessionStatusBadge|user-session-status"` → empty.
- [ ] **Step 4: Commit**

```bash
git add client/dashboard/src/lib/user-session-status.ts client/dashboard/src/components/sessions/SessionStatusBadge.tsx
git commit -m "feat(dashboard): shared session status presentation + badge"
```

### Task 7: Extract shared SessionRow (context menu + ⋮ + revoke)

**Files:** Create `client/dashboard/src/components/sessions/SessionRow.tsx`; Modify `pages/connect/UserSessions.tsx`, `components/project/UserSessionsCard.tsx`.

- [ ] **Step 1: Create `SessionRow.tsx`** — move the row + revoke dialog out of `UserSessions.tsx`, add the context menu + ⋮ menu. Read the current `SessionRow` in `UserSessions.tsx` first and port its dialog/revoke logic verbatim, then wrap:

```tsx
import { useState } from "react";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { MoreActions } from "@/components/ui/more-actions";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { cn } from "@/lib/utils";
import { SessionStatusBadge } from "./SessionStatusBadge";
import {
  sessionStatus,
  subjectLabel,
  STATUS_PRESENTATION,
} from "@/lib/user-session-status";
import { useRevokeUserSessionMutation } from "@gram/client/react-query";
import type { UserSession } from "@gram/client/models/components";
import { format, formatDistanceToNow } from "date-fns";

export function SessionRow({
  session,
  onRevoked,
}: {
  session: UserSession;
  onRevoked: () => void;
}): JSX.Element {
  const [confirmOpen, setConfirmOpen] = useState(false);
  const revoke = useRevokeUserSessionMutation();
  const status = sessionStatus(session);
  const canRevoke = status === "active";

  const doRevoke = () =>
    revoke.mutate(
      { request: { id: session.id } },
      {
        onSuccess: () => {
          setConfirmOpen(false);
          onRevoked();
        },
      },
    );

  const row = (
    <li className="flex items-center gap-3 px-3 py-2">
      <span
        className={cn(
          "size-2 shrink-0 rounded-full",
          STATUS_PRESENTATION[status].dotClass,
        )}
      />
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-medium">{subjectLabel(session)}</p>
        <p className="text-muted-foreground truncate text-xs">
          {session.clientName ? `${session.clientName} · ` : ""}
          gated by {session.issuerSlug}
        </p>
      </div>
      <SessionStatusBadge session={session} />
      <span className="text-muted-foreground shrink-0 text-xs">
        {status === "revoked" && session.revokedAt
          ? `revoked ${format(new Date(session.revokedAt), "PP")}`
          : `expires ${formatDistanceToNow(new Date(session.expiresAt), { addSuffix: true })}`}
      </span>
      {canRevoke && (
        <MoreActions
          actions={[
            {
              label: "Revoke",
              destructive: true,
              onClick: () => setConfirmOpen(true),
            },
          ]}
        />
      )}
    </li>
  );

  return (
    <>
      {canRevoke ? (
        <ContextMenu>
          <ContextMenuTrigger asChild>{row}</ContextMenuTrigger>
          <ContextMenuContent>
            <ContextMenuItem
              variant="destructive"
              onSelect={() => setConfirmOpen(true)}
            >
              Revoke session
            </ContextMenuItem>
          </ContextMenuContent>
        </ContextMenu>
      ) : (
        row
      )}

      <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Revoke session?</Dialog.Title>
            <Dialog.Description>
              This immediately invalidates the session for{" "}
              {subjectLabel(session)}. The client will need to re-authenticate.
            </Dialog.Description>
          </Dialog.Header>
          <Dialog.Footer>
            <Button variant="ghost" onClick={() => setConfirmOpen(false)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              disabled={revoke.isPending}
              onClick={doRevoke}
            >
              {revoke.isPending ? "Revoking…" : "Revoke"}
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
  );
}
```

Confirm exact APIs against the real files: `MoreActions` props (`actions: {label,onClick,destructive?,icon?}[]`), `ContextMenuItem` `onSelect` vs `onClick` and `variant`, `ContextMenuTrigger asChild` support, and that `Dialog` here does NOT need a `Dialog.Trigger` when controlled (the base page's revoke used `Dialog.Trigger asChild`; if a trigger is required, the `Dialog` can wrap an invisible trigger or use the controlled `open` prop — match how the base `UserSessions.tsx` did it).

- [ ] **Step 2: Refactor `UserSessions.tsx`** to import `SessionRow` from `@/components/sessions/SessionRow` and delete its local `SessionRow` + `STATUS_DOT`. Keep the page's list/filter/pagination shell.

- [ ] **Step 3: Refactor `UserSessionsCard.tsx`** to use `STATUS_PRESENTATION[...]` (or `SessionStatusBadge`) instead of its local `STATUS_DOT` map; remove the duplicate map.

- [ ] **Step 4: Type-check** `cd client/dashboard && ./node_modules/.bin/tsc -b --noEmit --force 2>&1 | grep -E "sessions/SessionRow|connect/UserSessions|UserSessionsCard|user-session-status"` → empty.

- [ ] **Step 5: Commit**

```bash
git add client/dashboard/src/components/sessions/SessionRow.tsx client/dashboard/src/pages/connect/UserSessions.tsx client/dashboard/src/components/project/UserSessionsCard.tsx
git commit -m "refactor(dashboard): shared SessionRow with context-menu + ⋮ revoke"
```

---

## WORKSTREAM 3 — SURFACES

### Task 8: Page facet filters (status, client, user, server)

**Files:** Modify `client/dashboard/src/pages/connect/UserSessions.tsx`.

- [ ] **Step 1: Add the filters.** Read the current page first. Add state for `clientId`, `subjectUrn`, `issuerId` (alongside existing `status`), fetch facets, and render three `FacetSelect`s plus the existing status control. Reference `OrgAuditLogs.tsx:466-479` for the wire pattern. Code to add inside `UserSessionsInner`:

```tsx
import { FacetSelect } from "@/components/auditlogs/feed";
import {
  useUserSessions /* if used */,
  useUserSessionFacets,
} from "@gram/client/react-query";
// ...
const { data: facets } = useUserSessionFacets({});
const [clientId, setClientId] = useState<string>("all");
const [subjectUrn, setSubjectUrn] = useState<string>("all");
const [issuerId, setIssuerId] = useState<string>("all");

const {
  data,
  isPending,
  hasNextPage,
  fetchNextPage,
  isFetchingNextPage,
  refetch,
} = useUserSessionsInfinite({
  status,
  clientId: clientId === "all" ? undefined : clientId,
  subjectUrn: subjectUrn === "all" ? undefined : subjectUrn,
  userSessionIssuerId: issuerId === "all" ? undefined : issuerId,
});
```

And the filter row JSX (next to the status control):

```tsx
<div className="flex flex-wrap gap-2">
  <FacetSelect
    label="MCP server"
    value={issuerId}
    onValueChange={setIssuerId}
    placeholder="All servers"
    allLabel="All servers"
    options={facets?.servers ?? []}
  />
  <FacetSelect
    label="Client"
    value={clientId}
    onValueChange={setClientId}
    placeholder="All clients"
    allLabel="All clients"
    options={facets?.clients ?? []}
  />
  <FacetSelect
    label="User"
    value={subjectUrn}
    onValueChange={setSubjectUrn}
    placeholder="All users"
    allLabel="All users"
    options={facets?.users ?? []}
  />
</div>
```

Confirm `FacetSelect` is exported from `@/components/auditlogs/feed` and its `options` item shape matches `facets.servers` items (`{value, displayName, count}`); confirm the generated facets hook name (`useUserSessionFacets` per the design react-hook meta). Keep the existing Status control (buttons or a Select). Ensure `useUserSessionsInfinite` request type now includes `clientId`/`subjectUrn`/`userSessionIssuerId` (it does post-SDK regen).

- [ ] **Step 2: Type-check** filtered to the page → empty (no new errors).
- [ ] **Step 3: Commit**

```bash
git add client/dashboard/src/pages/connect/UserSessions.tsx
git commit -m "feat(dashboard): status/client/user/server filters on User Sessions page"
```

### Task 9: MCP Authentication-tab sessions panel

**Files:** Create `client/dashboard/src/pages/mcp/x/tabs/settings/sections/authentication/McpServerSessionsPanel.tsx`; Modify `AuthenticationSection.tsx`.

- [ ] **Step 1: Create the panel:**

```tsx
import { SettingsSection } from "../../SettingsSection";
import { SessionRow } from "@/components/sessions/SessionRow";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useUserSessionsInfinite } from "@gram/client/react-query";
import type { McpServer } from "@gram/client/models/components";

export function McpServerSessionsPanel({
  mcpServer,
}: {
  mcpServer: McpServer;
}): JSX.Element {
  const issuerId = mcpServer.userSessionIssuerId;
  const {
    data,
    isPending,
    hasNextPage,
    fetchNextPage,
    isFetchingNextPage,
    refetch,
  } = useUserSessionsInfinite(
    { userSessionIssuerId: issuerId, status: "active" },
    undefined,
    { enabled: !!issuerId },
  );
  const sessions = data?.pages.flatMap((p) => p.result.items) ?? [];

  return (
    <SettingsSection id="user-sessions">
      <SettingsSection.Header>
        <SettingsSection.Title>User sessions</SettingsSection.Title>
        <SettingsSection.Description>
          Active sessions clients hold into this server, established via OAuth.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          {!issuerId ? (
            <p className="text-muted-foreground text-sm">
              This server isn't gated by a session issuer.
            </p>
          ) : isPending ? (
            <div className="space-y-2">
              {Array.from({ length: 3 }).map((_, i) => (
                <Skeleton key={i} className="h-12 w-full" />
              ))}
            </div>
          ) : sessions.length === 0 ? (
            <p className="text-muted-foreground text-sm">No active sessions</p>
          ) : (
            <ul className="divide-border divide-y rounded-md border">
              {sessions.map((s) => (
                <SessionRow
                  key={s.id}
                  session={s}
                  onRevoked={() => void refetch()}
                />
              ))}
            </ul>
          )}
          {hasNextPage && (
            <div className="flex justify-center pt-2">
              <Button
                variant="ghost"
                size="sm"
                disabled={isFetchingNextPage}
                onClick={() => void fetchNextPage()}
              >
                {isFetchingNextPage ? "Loading…" : "Load more"}
              </Button>
            </div>
          )}
        </SettingsSection.Body>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
```

Confirm: the `useUserSessionsInfinite` 3rd-arg options object supports `{ enabled }` (TanStack pattern; check the generated hook signature — it may be `(request, security?, options?)`). Confirm the page-shape accessor (`p.result.items`) matches what `UserSessions.tsx` uses. Confirm `SettingsSection` relative import path from this file's location.

- [ ] **Step 2: Render it in `AuthenticationSection.tsx`** — import `McpServerSessionsPanel` and render it at the **bottom**, immediately after the closing `</SettingsSection>` of the authentication section (so it appears as the last block of the auth area). Since `AuthenticationSection` returns a single `<SettingsSection>`, wrap both in a fragment:

```tsx
return (
  <>
    <SettingsSection id={MCP_AUTHENTICATION_SECTION_ID}>
      {/* ...existing content unchanged... */}
    </SettingsSection>
    <McpServerSessionsPanel mcpServer={mcpServer} />
  </>
);
```

(Confirm the existing return is a single `<SettingsSection>...</SettingsSection>`; wrap it + the panel in `<>...</>`. The panel renders on every server; empty state covers no-issuer servers.)

- [ ] **Step 3: Type-check** filtered to `McpServerSessionsPanel|AuthenticationSection` → empty.
- [ ] **Step 4: Commit**

```bash
git add "client/dashboard/src/pages/mcp/x/tabs/settings/sections/authentication/McpServerSessionsPanel.tsx" "client/dashboard/src/pages/mcp/x/tabs/settings/sections/authentication/AuthenticationSection.tsx"
git commit -m "feat(dashboard): user sessions panel on MCP authentication tab"
```

### Task 10: Frontend gates + changeset

**Files:** Create `.changeset/user-sessions-enhancements.md`.

- [ ] **Step 1: Knip** — `cd client/dashboard && NODE_OPTIONS=--max-old-space-size=8192 pnpm knip 2>&1 | tail -40`. Fix only feature-related unused-export findings (e.g. ensure `STATUS_PRESENTATION`, `SessionStatusBadge`, `SessionRow` are all consumed).
- [ ] **Step 2: Lint + format** — run the dashboard's `pnpm oxlint` + `pnpm oxfmt` (discover via `grep -E '"(lint|format|oxlint|oxfmt)"' client/dashboard/package.json`); fix feature-file issues only.
- [ ] **Step 3: Forced type-check** — `cd client/dashboard && ./node_modules/.bin/tsc -b --noEmit --force 2>&1 | grep -E "sessions/|connect/UserSessions|UserSessionsCard|authentication/Mcp|user-session-status"` → empty.
- [ ] **Step 4: Dashboard build** — `cd client/dashboard && NODE_OPTIONS=--max-old-space-size=8192 pnpm build` (run `pnpm -F @gram-ai/elements build` first if needed). Expect success or only documented-baseline failures.
- [ ] **Step 5: Changeset** — create `.changeset/user-sessions-enhancements.md`:

```md
---
"server": patch
---

User sessions enhancements: facet filters (status, client, user, MCP server) on the User Sessions page; a sessions panel on each MCP server's Authentication tab; revoke via right-click and ⋮ menus with brand-themed status badges; and two read-only assistant platform tools (list_user_sessions, get_user_session).
```

- [ ] **Step 6: Commit**

```bash
git add .changeset/user-sessions-enhancements.md
git commit -m "chore: changeset for user sessions enhancements"
```

---

## Final verification

- [ ] `cd server && go build ./... && DB_PORT=57912 go test ./internal/usersessions/ ./internal/mv/ ./internal/platformtools/usersessions/` — green.
- [ ] `cd client/dashboard && ./node_modules/.bin/tsc -b --noEmit --force` — no NEW feature-file errors.
- [ ] `pnpm knip` — no new failures.
- [ ] Manual: User Sessions page shows 4 filters that narrow the list; right-click + ⋮ both revoke (with confirm); status badges color-coded; every MCP server's Auth tab shows the sessions panel (empty state when no issuer); assistant can call `platform_list_user_sessions` / `platform_get_user_session` and gets project-scoped, token-free data.

## Self-review notes

- Spec coverage: #1 Task 9; #2 Tasks 1-3,5,8; #3 Task 7; #4 Tasks 6-7; #5 Task 4. All covered.
- Type consistency: `STATUS_PRESENTATION` (Task 6) consumed in Tasks 7,9; `SessionRow` (Task 7) consumed in Tasks 8,9; facet hook `useUserSessionFacets` + `clientId`/`subjectUrn`/`userSessionIssuerId` request fields (Tasks 2,5) consumed in Task 8.
- Residual confirmations called out inline (exact `core.*` helper signatures, `MoreActions`/`ContextMenu` props, `useUserSessionsInfinite` options arg, `Dialog` controlled vs trigger) — implementers verify against the cited existing files before finalizing each task.
