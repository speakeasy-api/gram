# User Sessions Feed Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Surface Gram-issued user sessions (clients connected into a project's issuer-gated toolsets) as a read-only widget on the project home and a filterable, revoke-capable page under Connect → User Sessions.

**Architecture:** The `userSessions` Goa service, its `listUserSessions`/`revokeUserSession` handlers, project-scoped RBAC (`project:read`/`project:write`), and the revoke audit event already exist. This work (a) enriches the list query with issuer slug, client name, and resolved subject identity plus a `status` filter; (b) widens the `UserSession` API type; (c) builds two React surfaces that consume the generated `useUserSessions` hook.

**Tech Stack:** Go, Goa (design-first codegen), sqlc (Postgres), pgx; React + TypeScript dashboard, TanStack Query via generated `@gram/client/react-query` hooks, Moonshine UI.

---

## Background: what already exists (do not rebuild)

- Service + handlers: `server/internal/usersessions/sessionhandlers.go`
  - `ListUserSessions` (lines ~26-73) already `Require`s `authz.ScopeProjectRead`, parses cursor/limit, filters by `subject_urn` + `user_session_issuer_id`, and computes `NextCursor` when `len(rows) >= limit`.
  - `RevokeUserSession` (lines ~77-139) already `Require`s `authz.ScopeProjectWrite`, soft-deletes, emits `s.audit.LogUserSessionRevoke`, and pushes the `jti` into the revocation cache.
- Design: `server/design/usersessions/design.go` — `UserSession` type (lines ~115-142), `listUserSessions` payload (lines ~18-53), `ListUserSessionsResult` (lines ~144-151).
- Query: `server/internal/usersessions/queries.sql` — `ListUserSessionsByProjectID` (lines ~170-185).
- Model view: `server/internal/mv/usersession.go` — `BuildUserSessionView` / `BuildUserSessionListView`.
- Generated hook: `useUserSessions` / `useUserSessionsInfinite` from `@gram/client/react-query` (`client/sdk/src/react-query/userSessions.ts`).
- Subject URN type: `server/internal/urn/session_subject.go` — `SessionSubject{ Kind, ID }`, kinds `user`/`apikey`/`anonymous`.

## File Structure

**Backend (modify):**

- `server/design/usersessions/design.go` — add 5 attributes to `UserSession`, add `status` filter to `listUserSessions`.
- `server/internal/usersessions/queries.sql` — enrich `ListUserSessionsByProjectID` SELECT + add `status` `CASE` filter.
- `server/internal/mv/usersession.go` — map new columns; derive subject identity + `revoked_at`.
- `server/internal/usersessions/sessionhandlers.go` — thread `payload.Status` into query params.
- Tests: `server/internal/mv/usersession_test.go` (new, pure unit), `server/internal/usersessions/listusersessions_test.go` (extend).
- Regenerated (do not hand-edit): `server/gen/**`, `server/internal/usersessions/repo/queries.sql.go`, `client/sdk/**`.

**Frontend (create + modify):**

- Create `client/dashboard/src/lib/user-session-status.ts` — status derivation helper.
- Create `client/dashboard/src/components/project/UserSessionsCard.tsx` — read-only home widget.
- Modify `client/dashboard/src/components/project/ProjectDashboard.tsx` — render the widget.
- Create `client/dashboard/src/pages/connect/UserSessions.tsx` — full filterable page with revoke.
- Modify `client/dashboard/src/routes.tsx` — register the `userSessions` route.
- Modify `client/dashboard/src/components/app-sidebar.tsx` — add nav item to the Connect group.

---

## Task 1: Extend the Goa design (UserSession type + status filter)

**Files:**

- Modify: `server/design/usersessions/design.go`

- [ ] **Step 1: Add enrichment attributes to the `UserSession` type**

In `server/design/usersessions/design.go`, inside `var UserSession = Type("UserSession", func() {...})`, add these attributes after the existing `updated_at` attribute (before the `Required(...)` call):

```go
	Attribute("issuer_slug", String, "Slug of the user_session_issuer that gated this session.")
	Attribute("client_name", String, "Name of the MCP client that established the session, if known.")
	Attribute("subject_type", String, "Subject kind: 'user', 'apikey', or 'anonymous'.")
	Attribute("subject_display_name", String, "Resolved human-readable name of the subject, if known.")
	Attribute("revoked_at", String, "When the session was revoked, if it has been.", func() {
		Format(FormatDateTime)
	})
```

Then update the `Required(...)` line in that type to add the two always-present fields:

```go
	Required("id", "user_session_issuer_id", "subject_urn", "jti", "refresh_expires_at", "expires_at", "created_at", "updated_at", "issuer_slug", "subject_type")
```

(`client_name`, `subject_display_name`, `revoked_at` stay optional → generated as `*string`.)

- [ ] **Step 2: Add the `status` filter to `listUserSessions`**

In the `Method("listUserSessions", ...)` `Payload(func() {...})`, add after the `user_session_issuer_id` attribute:

```go
		Attribute("status", String, "Filter by session status.", func() {
			Enum("active", "expired", "revoked", "all")
		})
```

In the same method's `HTTP(func() {...})`, add after `Param("user_session_issuer_id")`:

```go
		Param("status")
```

- [ ] **Step 3: Regenerate Goa server code**

Run: `mise gen:goa-server`
Expected: success; `server/gen/user_sessions/*.go` now has `IssuerSlug string`, `ClientName *string`, `SubjectType string`, `SubjectDisplayName *string`, `RevokedAt *string` on `types.UserSession`, and `Status *string` on `ListUserSessionsPayload`.

- [ ] **Step 4: Commit**

```bash
git add server/design/usersessions/design.go server/gen
git commit -m "feat(server): add enrichment + status filter to userSessions design"
```

---

## Task 2: Enrich the list query and add the status filter

**Files:**

- Modify: `server/internal/usersessions/queries.sql:170-185` (`ListUserSessionsByProjectID`)
- Regenerated: `server/internal/usersessions/repo/queries.sql.go`

- [ ] **Step 1: Replace the `ListUserSessionsByProjectID` query body**

Replace the query (keep the `-- name:` line and the two comment lines above the SELECT) with:

```sql
-- name: ListUserSessionsByProjectID :many
-- refresh_token_hash is excluded from the projection so the management API
-- surface cannot accidentally return it.
SELECT s.id, s.user_session_issuer_id, s.user_session_client_id, s.subject_urn, s.jti,
       s.refresh_expires_at, s.expires_at,
       s.created_at, s.updated_at, s.deleted_at, s.deleted,
       iss.slug AS issuer_slug,
       c.client_name AS client_name,
       u.display_name AS user_display_name,
       u.email AS user_email,
       k.name AS api_key_name
FROM user_sessions AS s
JOIN user_session_issuers AS iss ON iss.id = s.user_session_issuer_id
LEFT JOIN user_session_clients AS c ON c.id = s.user_session_client_id
LEFT JOIN users AS u
  ON s.subject_urn::text LIKE 'user:%'
  AND u.id = split_part(s.subject_urn::text, ':', 2)
LEFT JOIN api_keys AS k
  ON k.id = CASE
             WHEN s.subject_urn::text LIKE 'apikey:%'
             THEN split_part(s.subject_urn::text, ':', 2)::uuid
           END
WHERE iss.project_id = @project_id
  AND iss.deleted IS FALSE
  AND CASE sqlc.narg('status')::text
        WHEN 'active'  THEN (s.deleted IS FALSE AND s.expires_at > now())
        WHEN 'expired' THEN (s.deleted IS FALSE AND s.expires_at <= now())
        WHEN 'revoked' THEN (s.deleted IS TRUE)
        WHEN 'all'     THEN TRUE
        ELSE (s.deleted IS FALSE)
      END
  AND (sqlc.narg('subject_urn')::text IS NULL OR s.subject_urn = sqlc.narg('subject_urn')::text)
  AND (sqlc.narg('user_session_issuer_id')::uuid IS NULL OR s.user_session_issuer_id = sqlc.narg('user_session_issuer_id')::uuid)
  AND (sqlc.narg('cursor')::uuid IS NULL OR s.id < sqlc.narg('cursor')::uuid)
ORDER BY s.id DESC
LIMIT sqlc.arg('limit_value');
```

Notes for the implementer:

- The api_keys `::uuid` cast is wrapped in `CASE WHEN ... LIKE 'apikey:%'` so it is only evaluated for apikey subjects (avoids "invalid input syntax for type uuid" on `user:`/`anonymous:` rows).
- `split_part(...,':',2)` is safe because `ParseSessionSubject` guarantees a single `kind:id` split for user/apikey ids (no embedded colon).
- Default (NULL `status`) preserves the previous behaviour: live sessions only.

- [ ] **Step 2: Regenerate sqlc**

Run: `mise gen:sqlc-server`
Expected: `ListUserSessionsByProjectIDRow` now includes `IssuerSlug string`, `ClientName pgtype.Text`, `UserDisplayName pgtype.Text`, `UserEmail pgtype.Text`, `ApiKeyName pgtype.Text`; `ListUserSessionsByProjectIDParams` now includes `Status pgtype.Text`.

- [ ] **Step 3: Verify the generated Row/Params shape**

Run: `grep -n "IssuerSlug\|UserDisplayName\|ApiKeyName\|Status " server/internal/usersessions/repo/queries.sql.go`
Expected: the new fields appear on the Row struct and `Status pgtype.Text` on the Params struct. (If any enrichment column generated as `interface{}`, stop — the `AS` alias should keep them as real column types; do not proceed with `interface{}`.)

- [ ] **Step 4: Commit**

```bash
git add server/internal/usersessions/queries.sql server/internal/usersessions/repo/queries.sql.go
git commit -m "feat(server): enrich user session list query with identity + status filter"
```

---

## Task 3: Map enrichment in the model view (TDD, pure unit test)

**Files:**

- Test: `server/internal/mv/usersession_test.go` (create)
- Modify: `server/internal/mv/usersession.go`

- [ ] **Step 1: Write the failing unit test**

Create `server/internal/mv/usersession_test.go`:

```go
package mv

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func ts(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func TestBuildUserSessionView_ResolvesUser(t *testing.T) {
	t.Parallel()

	row := repo.ListUserSessionsByProjectIDRow{
		ID:                  uuid.New(),
		UserSessionIssuerID: uuid.New(),
		SubjectUrn:          urn.NewUserSubject("user-123"),
		Jti:                 "jti-1",
		RefreshExpiresAt:    ts(time.Now()),
		ExpiresAt:           ts(time.Now()),
		CreatedAt:           ts(time.Now()),
		UpdatedAt:           ts(time.Now()),
		IssuerSlug:          "my-issuer",
		ClientName:          pgtype.Text{String: "Claude Desktop", Valid: true},
		UserDisplayName:     pgtype.Text{String: "Ada Lovelace", Valid: true},
		UserEmail:           pgtype.Text{String: "ada@example.com", Valid: true},
		Deleted:             false,
	}

	got := BuildUserSessionView(row)

	require.Equal(t, "my-issuer", got.IssuerSlug)
	require.Equal(t, "user", got.SubjectType)
	require.NotNil(t, got.ClientName)
	require.Equal(t, "Claude Desktop", *got.ClientName)
	require.NotNil(t, got.SubjectDisplayName)
	require.Equal(t, "Ada Lovelace", *got.SubjectDisplayName)
	require.Nil(t, got.RevokedAt)
}

func TestBuildUserSessionView_UserFallsBackToEmail(t *testing.T) {
	t.Parallel()

	row := repo.ListUserSessionsByProjectIDRow{
		ID:               uuid.New(),
		SubjectUrn:       urn.NewUserSubject("user-123"),
		RefreshExpiresAt: ts(time.Now()), ExpiresAt: ts(time.Now()),
		CreatedAt: ts(time.Now()), UpdatedAt: ts(time.Now()),
		IssuerSlug:      "iss",
		UserDisplayName: pgtype.Text{Valid: false},
		UserEmail:       pgtype.Text{String: "ada@example.com", Valid: true},
	}

	got := BuildUserSessionView(row)
	require.NotNil(t, got.SubjectDisplayName)
	require.Equal(t, "ada@example.com", *got.SubjectDisplayName)
}

func TestBuildUserSessionView_APIKeyAndRevoked(t *testing.T) {
	t.Parallel()

	revokedAt := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	row := repo.ListUserSessionsByProjectIDRow{
		ID:               uuid.New(),
		SubjectUrn:       urn.NewAPIKeySubject(uuid.New()),
		RefreshExpiresAt: ts(time.Now()), ExpiresAt: ts(time.Now()),
		CreatedAt: ts(time.Now()), UpdatedAt: ts(time.Now()),
		IssuerSlug: "iss",
		ApiKeyName: pgtype.Text{String: "ci-key", Valid: true},
		DeletedAt:  ts(revokedAt),
		Deleted:    true,
	}

	got := BuildUserSessionView(row)
	require.Equal(t, "apikey", got.SubjectType)
	require.NotNil(t, got.SubjectDisplayName)
	require.Equal(t, "ci-key", *got.SubjectDisplayName)
	require.NotNil(t, got.RevokedAt)
	require.Equal(t, revokedAt.Format(time.RFC3339), *got.RevokedAt)
}

func TestBuildUserSessionView_AnonymousHasNoName(t *testing.T) {
	t.Parallel()

	row := repo.ListUserSessionsByProjectIDRow{
		ID:               uuid.New(),
		SubjectUrn:       urn.NewAnonymousSubject("mcp-sess-1"),
		RefreshExpiresAt: ts(time.Now()), ExpiresAt: ts(time.Now()),
		CreatedAt: ts(time.Now()), UpdatedAt: ts(time.Now()),
		IssuerSlug: "iss",
	}

	got := BuildUserSessionView(row)
	require.Equal(t, "anonymous", got.SubjectType)
	require.Nil(t, got.SubjectDisplayName)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd server && go test ./internal/mv/ -run TestBuildUserSessionView -v`
Expected: FAIL — compile error (unknown fields `IssuerSlug`/`SubjectType`/etc. on `types.UserSession`, or mismatched mapping). This confirms the test targets the new contract.

- [ ] **Step 3: Implement the enrichment mapping**

Replace `server/internal/mv/usersession.go` with:

```go
package mv

import (
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func resolveSubject(row repo.ListUserSessionsByProjectIDRow) (subjectType string, displayName *string) {
	subjectType = string(row.SubjectUrn.Kind)
	switch row.SubjectUrn.Kind {
	case urn.SessionSubjectKindUser:
		if name := conv.FromPGText[string](row.UserDisplayName); name != nil && *name != "" {
			return subjectType, name
		}
		return subjectType, conv.FromPGText[string](row.UserEmail)
	case urn.SessionSubjectKindAPIKey:
		return subjectType, conv.FromPGText[string](row.ApiKeyName)
	default:
		return subjectType, nil
	}
}

func BuildUserSessionView(row repo.ListUserSessionsByProjectIDRow) *types.UserSession {
	subjectType, subjectName := resolveSubject(row)

	var revokedAt *string
	if row.Deleted && row.DeletedAt.Valid {
		s := row.DeletedAt.Time.Format(time.RFC3339)
		revokedAt = &s
	}

	return &types.UserSession{
		ID:                  row.ID.String(),
		UserSessionIssuerID: row.UserSessionIssuerID.String(),
		SubjectUrn:          row.SubjectUrn.String(),
		Jti:                 row.Jti,
		RefreshExpiresAt:    row.RefreshExpiresAt.Time.Format(time.RFC3339),
		ExpiresAt:           row.ExpiresAt.Time.Format(time.RFC3339),
		CreatedAt:           row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:           row.UpdatedAt.Time.Format(time.RFC3339),
		IssuerSlug:          row.IssuerSlug,
		ClientName:          conv.FromPGText[string](row.ClientName),
		SubjectType:         subjectType,
		SubjectDisplayName:  subjectName,
		RevokedAt:           revokedAt,
	}
}

func BuildUserSessionListView(rows []repo.ListUserSessionsByProjectIDRow) []*types.UserSession {
	out := make([]*types.UserSession, len(rows))
	for i, row := range rows {
		out[i] = BuildUserSessionView(row)
	}
	return out
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd server && go test ./internal/mv/ -run TestBuildUserSessionView -v`
Expected: PASS (all four subtests).

- [ ] **Step 5: Commit**

```bash
git add server/internal/mv/usersession.go server/internal/mv/usersession_test.go
git commit -m "feat(server): resolve subject identity in user session view"
```

---

## Task 4: Thread the status filter through the handler (TDD)

**Files:**

- Test: `server/internal/usersessions/listusersessions_test.go` (extend)
- Modify: `server/internal/usersessions/sessionhandlers.go`

- [ ] **Step 1: Write failing handler tests for enrichment + status**

Append to `server/internal/usersessions/listusersessions_test.go` (the file already imports `issuersgen`, `gen`, `urn`, `uuid`, `require`):

```go
func TestListUserSessions_ReturnsEnrichment(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		Slug: "enrichment-issuer", AuthnChallengeMode: "chain", SessionDurationHours: 24,
	})
	require.NoError(t, err)

	_, err = seedUserSession(t, ctx, ti.conn, uuid.MustParse(issuer.ID), urn.NewUserSubject("nobody"))
	require.NoError(t, err)

	got, err := ti.service.ListUserSessions(ctx, &gen.ListUserSessionsPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		SubjectUrn: nil, UserSessionIssuerID: nil, Cursor: nil, Limit: nil, Status: nil,
	})
	require.NoError(t, err)
	require.Len(t, got.Items, 1)
	require.Equal(t, "enrichment-issuer", got.Items[0].IssuerSlug)
	require.Equal(t, "user", got.Items[0].SubjectType)
	// "nobody" is not a real users.id, so the LEFT JOIN resolves to nothing.
	require.Nil(t, got.Items[0].SubjectDisplayName)
	require.Nil(t, got.Items[0].RevokedAt)
}

func TestListUserSessions_StatusRevokedFilter(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		Slug: "status-issuer", AuthnChallengeMode: "chain", SessionDurationHours: 24,
	})
	require.NoError(t, err)

	live, err := seedUserSession(t, ctx, ti.conn, uuid.MustParse(issuer.ID), urn.NewUserSubject("live"))
	require.NoError(t, err)
	toRevoke, err := seedUserSession(t, ctx, ti.conn, uuid.MustParse(issuer.ID), urn.NewUserSubject("dead"))
	require.NoError(t, err)

	require.NoError(t, ti.service.RevokeUserSession(ctx, &gen.RevokeUserSessionPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, ID: toRevoke.ID.String(),
	}))

	active := "active"
	gotActive, err := ti.service.ListUserSessions(ctx, &gen.ListUserSessionsPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		SubjectUrn: nil, UserSessionIssuerID: nil, Cursor: nil, Limit: nil, Status: &active,
	})
	require.NoError(t, err)
	require.Len(t, gotActive.Items, 1)
	require.Equal(t, live.ID.String(), gotActive.Items[0].ID)

	revoked := "revoked"
	gotRevoked, err := ti.service.ListUserSessions(ctx, &gen.ListUserSessionsPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		SubjectUrn: nil, UserSessionIssuerID: nil, Cursor: nil, Limit: nil, Status: &revoked,
	})
	require.NoError(t, err)
	require.Len(t, gotRevoked.Items, 1)
	require.Equal(t, toRevoke.ID.String(), gotRevoked.Items[0].ID)
	require.NotNil(t, gotRevoked.Items[0].RevokedAt)

	all := "all"
	gotAll, err := ti.service.ListUserSessions(ctx, &gen.ListUserSessionsPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		SubjectUrn: nil, UserSessionIssuerID: nil, Cursor: nil, Limit: nil, Status: &all,
	})
	require.NoError(t, err)
	require.Len(t, gotAll.Items, 2)
}
```

Notes: `seedUserSession` returns the seeded row (its `.ID` is used above); confirm its return type exposes `.ID` (it is used as `_, err :=` elsewhere — capture the first return value here). If `RevokeUserSessionPayload` has fields beyond those shown, fill them with `nil` to match the generated struct.

- [ ] **Step 2: Run to verify failure**

Run: `cd server && go test ./internal/usersessions/ -run 'TestListUserSessions_(ReturnsEnrichment|StatusRevokedFilter)' -v`
Expected: FAIL — `Status` field not yet passed through (revoked/all return 0/empty or compile error on `Status` param if handler not updated).

- [ ] **Step 3: Pass the status param through the handler**

In `server/internal/usersessions/sessionhandlers.go`, in `ListUserSessions`, update the `repo.ListUserSessionsByProjectIDParams{...}` literal to add the `Status` field:

```go
	rows, err := repo.New(s.db).ListUserSessionsByProjectID(ctx, repo.ListUserSessionsByProjectIDParams{
		ProjectID:           *authCtx.ProjectID,
		SubjectUrn:          conv.PtrToPGTextEmpty(payload.SubjectUrn),
		UserSessionIssuerID: issuerFilter,
		Cursor:              cursor,
		LimitValue:          limit,
		Status:              conv.PtrToPGTextEmpty(payload.Status),
	})
```

- [ ] **Step 4: Run to verify pass**

Run: `cd server && go test ./internal/usersessions/ -run 'TestListUserSessions' -v`
Expected: PASS (existing tests + the two new ones).

- [ ] **Step 5: Build the server**

Run: `mise build:server`
Expected: build succeeds.

- [ ] **Step 6: Commit**

```bash
git add server/internal/usersessions/sessionhandlers.go server/internal/usersessions/listusersessions_test.go
git commit -m "feat(server): thread status filter through ListUserSessions handler"
```

---

## Task 5: Regenerate the SDK

**Files:**

- Regenerated: `client/sdk/**`

- [ ] **Step 1: Regenerate the SDK**

Run: `mise gen:sdk`
Expected: `client/sdk` models/operations updated; `UserSession` model gains `issuerSlug`, `clientName?`, `subjectType`, `subjectDisplayName?`, `revokedAt?`; `ListUserSessionsRequest` gains `status?`.

- [ ] **Step 2: Build the SDK (required before dashboard type-check resolves the new fields)**

Run: `cd client/sdk && pnpm build`
Expected: build succeeds. (Per project notes, the dashboard resolves SDK `.d.ts` from the built output; skipping this makes the new fields appear missing.)

- [ ] **Step 3: Commit**

```bash
git add client/sdk
git commit -m "chore(sdk): regenerate for user session enrichment + status filter"
```

---

## Task 6: Project-home widget (read-only)

**Files:**

- Create: `client/dashboard/src/lib/user-session-status.ts`
- Create: `client/dashboard/src/components/project/UserSessionsCard.tsx`
- Modify: `client/dashboard/src/components/project/ProjectDashboard.tsx`

- [ ] **Step 1: Create the status helper**

Create `client/dashboard/src/lib/user-session-status.ts`:

```ts
import type { UserSession } from "@gram/client/models/components";

export type SessionStatus = "active" | "expired" | "revoked";

export function sessionStatus(session: UserSession): SessionStatus {
  if (session.revokedAt) return "revoked";
  if (new Date(session.expiresAt).getTime() <= Date.now()) return "expired";
  return "active";
}

export function subjectLabel(session: UserSession): string {
  if (session.subjectDisplayName) return session.subjectDisplayName;
  switch (session.subjectType) {
    case "apikey":
      return "API key";
    case "anonymous":
      return "Anonymous client";
    default:
      return session.subjectUrn;
  }
}
```

- [ ] **Step 2: Create the `UserSessionsCard` widget**

Create `client/dashboard/src/components/project/UserSessionsCard.tsx`:

```tsx
import { ChevronRight } from "lucide-react";
import { Link } from "react-router";
import { useUserSessions } from "@gram/client/react-query";
import { DashboardCard } from "@/components/ui/dashboard-card";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { sessionStatus, subjectLabel } from "@/lib/user-session-status";
import { formatDistanceToNow } from "date-fns";

const STATUS_DOT: Record<string, string> = {
  active: "bg-emerald-500",
  expired: "bg-muted-foreground",
  revoked: "bg-destructive",
};

export function UserSessionsCard({
  viewAllHref,
}: {
  viewAllHref: string;
}): JSX.Element {
  const { data, isPending } = useUserSessions({ status: "active", limit: 5 });
  const sessions = data?.items ?? [];

  return (
    <DashboardCard
      title="User Sessions"
      tooltip="Active sessions clients hold into this project's MCP servers, established via OAuth. Most recent first."
      action={
        <Link
          to={viewAllHref}
          className="text-muted-foreground hover:text-foreground flex items-center gap-0.5 text-xs no-underline"
        >
          View all
          <ChevronRight className="size-3" />
        </Link>
      }
    >
      {isPending ? (
        <div className="space-y-3">
          {Array.from({ length: 5 }).map((_, i) => (
            <Skeleton key={i} className="h-10 w-full" />
          ))}
        </div>
      ) : sessions.length === 0 ? (
        <p className="text-muted-foreground text-sm">No active sessions</p>
      ) : (
        <ul className="divide-border divide-y">
          {sessions.map((s) => (
            <li key={s.id} className="flex items-center gap-3 py-2">
              <span
                className={cn(
                  "size-2 shrink-0 rounded-full",
                  STATUS_DOT[sessionStatus(s)],
                )}
              />
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium">
                  {subjectLabel(s)}
                </p>
                <p className="text-muted-foreground truncate text-xs">
                  {s.clientName ? `${s.clientName} · ` : ""}
                  {s.issuerSlug}
                </p>
              </div>
              <span className="text-muted-foreground shrink-0 text-xs">
                expires{" "}
                {formatDistanceToNow(new Date(s.expiresAt), {
                  addSuffix: true,
                })}
              </span>
            </li>
          ))}
        </ul>
      )}
    </DashboardCard>
  );
}
```

- [ ] **Step 3: Render the widget in `ProjectDashboard`**

In `client/dashboard/src/components/project/ProjectDashboard.tsx`:

1. Add the import near the existing `ActivityTimelineCard` import (line ~38):

```tsx
import { UserSessionsCard } from "./UserSessionsCard";
```

2. Locate the `<ActivityTimelineCard ... />` usage (lines ~664-668) and render the new card immediately after it, using the same project route helper used elsewhere on this page for the User Sessions page href. Use the route added in Task 7:

```tsx
<UserSessionsCard viewAllHref={routes.userSessions.href()} />
```

(If the page's route object is named differently in scope — confirm how `viewAllHref={orgRoutes.auditLogs.href()}` resolves `orgRoutes`/`routes` in this file and mirror that to reach `routes.userSessions.href()` from the project route set.)

- [ ] **Step 4: Type-check (forced, to bypass the incremental cache)**

Run: `cd client/dashboard && ./node_modules/.bin/tsc -b --noEmit --force`
Expected: no new errors referencing `UserSessionsCard`, `useUserSessions`, or `routes.userSessions`. (Baseline pre-existing errors per project notes may remain; do not introduce new ones.)

- [ ] **Step 5: Commit**

```bash
git add client/dashboard/src/lib/user-session-status.ts client/dashboard/src/components/project/UserSessionsCard.tsx client/dashboard/src/components/project/ProjectDashboard.tsx
git commit -m "feat(dashboard): add read-only user sessions widget to project home"
```

---

## Task 7: Connect → User Sessions page (filters + revoke)

**Files:**

- Create: `client/dashboard/src/pages/connect/UserSessions.tsx`
- Modify: `client/dashboard/src/routes.tsx`
- Modify: `client/dashboard/src/components/app-sidebar.tsx`

- [ ] **Step 1: Create the page**

Create `client/dashboard/src/pages/connect/UserSessions.tsx`:

```tsx
import { useState } from "react";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { sessionStatus, subjectLabel } from "@/lib/user-session-status";
import {
  useUserSessionsInfinite,
  useRevokeUserSessionMutation,
} from "@gram/client/react-query";
import type { UserSession } from "@gram/client/models/components";
import { format, formatDistanceToNow } from "date-fns";

const STATUS_OPTIONS = ["all", "active", "expired", "revoked"] as const;
type StatusFilter = (typeof STATUS_OPTIONS)[number];

const STATUS_DOT: Record<string, string> = {
  active: "bg-emerald-500",
  expired: "bg-muted-foreground",
  revoked: "bg-destructive",
};

export default function UserSessions(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope="project:read" level="page">
          <UserSessionsInner />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

function UserSessionsInner(): JSX.Element {
  const [status, setStatus] = useState<StatusFilter>("all");
  const {
    data,
    isPending,
    hasNextPage,
    fetchNextPage,
    isFetchingNextPage,
    refetch,
  } = useUserSessionsInfinite({ status });
  const sessions = data?.pages.flatMap((p) => p.items) ?? [];

  return (
    <div className="space-y-4">
      <div className="flex gap-2">
        {STATUS_OPTIONS.map((opt) => (
          <Button
            key={opt}
            variant={status === opt ? "secondary" : "ghost"}
            size="sm"
            onClick={() => setStatus(opt)}
          >
            {opt[0].toUpperCase() + opt.slice(1)}
          </Button>
        ))}
      </div>

      {isPending ? (
        <div className="space-y-2">
          {Array.from({ length: 8 }).map((_, i) => (
            <Skeleton key={i} className="h-12 w-full" />
          ))}
        </div>
      ) : sessions.length === 0 ? (
        <p className="text-muted-foreground text-sm">No sessions found</p>
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
        <div className="flex justify-center">
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
    </div>
  );
}

function SessionRow({
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

  return (
    <li className="flex items-center gap-3 px-3 py-2">
      <span
        className={cn("size-2 shrink-0 rounded-full", STATUS_DOT[status])}
      />
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-medium">{subjectLabel(session)}</p>
        <p className="text-muted-foreground truncate text-xs">
          {session.clientName ? `${session.clientName} · ` : ""}
          gated by {session.issuerSlug}
        </p>
      </div>
      <span className="text-muted-foreground shrink-0 text-xs">
        {status === "revoked" && session.revokedAt
          ? `revoked ${format(new Date(session.revokedAt), "PP")}`
          : `expires ${formatDistanceToNow(new Date(session.expiresAt), { addSuffix: true })}`}
      </span>
      {canRevoke && (
        <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
          <Button
            variant="destructive"
            size="sm"
            onClick={() => setConfirmOpen(true)}
          >
            Revoke
          </Button>
          <Dialog.Content>
            <Dialog.Header>
              <Dialog.Title>Revoke session?</Dialog.Title>
              <Dialog.Description>
                This immediately invalidates the session for{" "}
                {subjectLabel(session)}. The client will need to
                re-authenticate.
              </Dialog.Description>
            </Dialog.Header>
            <Dialog.Footer>
              <Button variant="ghost" onClick={() => setConfirmOpen(false)}>
                Cancel
              </Button>
              <Button
                variant="destructive"
                disabled={revoke.isPending}
                onClick={() =>
                  revoke.mutate(
                    { request: { id: session.id } },
                    {
                      onSuccess: () => {
                        setConfirmOpen(false);
                        onRevoked();
                      },
                    },
                  )
                }
              >
                {revoke.isPending ? "Revoking…" : "Revoke"}
              </Button>
            </Dialog.Footer>
          </Dialog.Content>
        </Dialog>
      )}
    </li>
  );
}
```

Verified facts (already confirmed against the generated SDK + UI kit — do not re-guess):

- Revoke hook is `useRevokeUserSessionMutation` (`@gram/client/react-query`); variables are `{ request: operations.RevokeUserSessionRequest }` and `RevokeUserSessionRequest` has `id: string` → `revoke.mutate({ request: { id: session.id } }, ...)` is correct.
- `useUserSessionsInfinite` (`@gram/client/react-query`) exists; pages expose `.items` (mirrors the list result `items` field).
- `Button` is `@/components/ui/button` (shadcn): plain children (no `<Button.Text>`), variants `default | destructive | destructiveGhost | outline | secondary | ghost | link`, sizes `default | inline | sm | lg | icon | icon-sm`. The code above uses only these.
- `Dialog` is `@/components/ui/dialog` with compound `Content`/`Header`/`Title`/`Description`/`Footer`.

- [ ] **Step 2: Register the route**

In `client/dashboard/src/routes.tsx`:

1. Add the import alongside the other page imports (near the `Deployments` import, ~line 19):

```tsx
import UserSessions from "./pages/connect/UserSessions";
```

2. Add a route object in the same project-routes object where `deployments` lives (mirror its shape):

```tsx
  userSessions: {
    title: "User Sessions",
    url: "user-sessions",
    icon: "users",
    component: UserSessions,
  },
```

(Confirm `"users"` is a valid icon name in the project's `Icon` set; if not, use an existing one such as `"key-round"` or `"plug"`.)

- [ ] **Step 3: Add the nav item to the Connect group**

In `client/dashboard/src/components/app-sidebar.tsx`, inside the Connect `CollapsibleNavGroup` (after the `deployments` `ScopeGatedNavItem`, ~line 211):

```tsx
<ScopeGatedNavItem
  item={routes.userSessions}
  scope={scopeFor(routes.userSessions)}
/>
```

Confirm `scopeFor` returns `project:read` for this route (it maps routes → scopes the same way it does for sibling Connect items); if `scopeFor` needs an explicit entry, add `userSessions → "project:read"` wherever the mapping is defined.

- [ ] **Step 4: Type-check (forced)**

Run: `cd client/dashboard && ./node_modules/.bin/tsc -b --noEmit --force`
Expected: no new errors. Resolve any mismatch in mutation/hook names surfaced here against the generated SDK.

- [ ] **Step 5: Commit**

```bash
git add client/dashboard/src/pages/connect/UserSessions.tsx client/dashboard/src/routes.tsx client/dashboard/src/components/app-sidebar.tsx
git commit -m "feat(dashboard): add Connect > User Sessions page with revoke"
```

---

## Task 8: Frontend verification gates + changeset

**Files:**

- Create: `.changeset/user-sessions-feed.md`

- [ ] **Step 1: Knip (unused exports gate)**

Run: `cd client/dashboard && NODE_OPTIONS=--max-old-space-size=8192 pnpm knip`
Expected: no new unused-export failures. If `subjectLabel`/`sessionStatus` is flagged, ensure both are imported where used (widget + page); remove any export with no consumer.

- [ ] **Step 2: Lint + format**

Run: `cd client/dashboard && pnpm oxlint && pnpm oxfmt` (or the repo's configured lint/format tasks)
Expected: clean. Note `noUncheckedIndexedAccess` is on — guard any array index access.

- [ ] **Step 3: Dashboard build**

Run: `cd client/dashboard && NODE_OPTIONS=--max-old-space-size=8192 pnpm build`
Expected: build succeeds. (If `@gram-ai/elements` fails to resolve in a fresh worktree, run `pnpm -F @gram-ai/elements build` first, per project notes.)

- [ ] **Step 4: Add a changeset (required for feat PRs)**

Create `.changeset/user-sessions-feed.md`:

```md
---
"server": patch
---

Add user sessions feed: enrich the userSessions list API with issuer slug, client name, resolved subject identity, and a status filter; surface a read-only widget on the project home and a filterable Connect > User Sessions page with revoke.
```

(If the dashboard package is independently versioned in this repo's changeset config, add its entry too; otherwise `server` patch covers the API change.)

- [ ] **Step 5: Commit**

```bash
git add .changeset/user-sessions-feed.md
git commit -m "chore: changeset for user sessions feed"
```

---

## Final verification

- [ ] `mise build:server` — backend compiles.
- [ ] `cd server && go test ./internal/mv/ ./internal/usersessions/` — all pass.
- [ ] `cd client/dashboard && ./node_modules/.bin/tsc -b --noEmit --force` — no new type errors.
- [ ] `cd client/dashboard && NODE_OPTIONS=--max-old-space-size=8192 pnpm knip` — no new failures.
- [ ] Manual: project home shows the read-only User Sessions widget (active only, ≤5 rows, "View all"); Connect → User Sessions lists all with status filter + working revoke confirm.

## Notes / gotchas (from project memory)

- Codegen order is fixed: `gen:goa-server` → `gen:sqlc-server` → `gen:sdk`. Commit regenerated files (`server/gen`, `repo/queries.sql.go`, `client/sdk`) — CI fails on stale generated output.
- No DB migration here (read-path only) — do not touch `server/migrations/`, `schema.sql`, or `atlas.sum`.
- Build the SDK (`cd client/sdk && pnpm build`) after `gen:sdk` or the dashboard type-check resolves stale `.d.ts` and reports the new fields as missing.
- Use forced type-check (`tsc -b --noEmit --force`) — the incremental cache can hide errors CI catches.
- In worktrees, the oxfmt pre-commit hook may fail; `git commit --no-verify` is acceptable for these commits if the hook binary is missing (run lint/format manually in Task 8 regardless).

```

```
