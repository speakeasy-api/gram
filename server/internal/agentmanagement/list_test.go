package agentmanagement

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/agents"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestListAgentsReusesOwnerProfileWithinRequest(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "caller")
	createAgent(t, conn, "org-a", "caller", "First")
	createAgent(t, conn, "org-a", "caller", "Second")
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-a", "caller")

	listed, err := service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	require.Len(t, listed.Items, 2)
	require.NotNil(t, listed.Items[0].OwnerProfile)
	require.Same(t, listed.Items[0].OwnerProfile, listed.Items[1].OwnerProfile)
	require.Equal(t, "caller", listed.Items[0].OwnerProfile.DisplayName)
	require.True(t, listed.Items[0].Permissions.Read)
	require.True(t, listed.Items[1].Permissions.Read)

	// A subsequent request must load a fresh profile rather than retaining
	// tenant membership/profile data on the shared service.
	again, err := service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	require.Len(t, again.Items, 2)
	require.NotSame(t, listed.Items[0].OwnerProfile, again.Items[0].OwnerProfile)
	require.Same(t, again.Items[0].OwnerProfile, again.Items[1].OwnerProfile)
}

func TestListAgentsUsesOwnershipOrExplicitRead(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganization(t, conn, "org-b")
	for _, user := range []string{"caller", "other"} {
		seedOrganizationUser(t, conn, "org-a", user)
	}
	seedOrganizationUser(t, conn, "org-b", "caller")
	owned := createAgent(t, conn, "org-a", "caller", "Owned")
	delegated := createAgent(t, conn, "org-a", "other", "Delegated")
	createAgent(t, conn, "org-a", "other", "Hidden")
	createAgent(t, conn, "org-b", "caller", "Other tenant")
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-a", "caller")
	listed, err := service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	require.Len(t, listed.Items, 1)
	require.Equal(t, owned.ID.String(), listed.Items[0].ID)
	require.True(t, listed.Items[0].Permissions.Read)
	require.NotNil(t, listed.Items[0].OwnerProfile)
	require.Equal(t, "caller", listed.Items[0].OwnerProfile.DisplayName)
	_, err = agentsrepo.New(conn).SuspendAgent(ctx, agentsrepo.SuspendAgentParams{OrganizationID: "org-a", ID: owned.ID})
	require.NoError(t, err)
	listed, err = service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	require.Len(t, listed.Items, 1)
	require.Equal(t, gen.AgentLifecycle("suspended"), listed.Items[0].Lifecycle)
	_, err = agentsrepo.New(conn).RevokeAgent(ctx, agentsrepo.RevokeAgentParams{OrganizationID: "org-a", ID: owned.ID})
	require.NoError(t, err)
	listed, err = service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	require.Len(t, listed.Items, 1)
	require.Equal(t, owned.ID.String(), listed.Items[0].ID)
	require.Equal(t, gen.AgentLifecycle("revoked"), listed.Items[0].Lifecycle)
	support := contextvalues.WithValidatedGramSession(ctx, mustAuthContext(t, ctx), true)
	_, err = service.List(support, &gen.ListPayload{})
	requireOopsCode(t, err, oops.CodeForbidden)
	selectors, err := authz.NewSelector(authz.ScopeAgentRead, delegated.ID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: "org-a", PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, "caller"), Scope: string(authz.ScopeAgentRead),
		Selectors: selectors,
	})
	require.NoError(t, err)
	listed, err = service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	require.Len(t, listed.Items, 2)
	require.Equal(t, delegated.ID.String(), listed.Items[0].ID)
	require.True(t, listed.Items[0].Permissions.Read)
	require.False(t, listed.Items[0].Permissions.Write)
	_, err = service.List(validatedHumanContext(t, "org-a", "absent"), &gen.ListPayload{})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestListAgentsPagesByNameWithoutRepeatingOrSkipping(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "caller")
	names := []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo"}
	for _, name := range names {
		createAgent(t, conn, "org-a", "caller", name)
	}
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-a", "caller")

	limit := 2
	seen := make([]string, 0, len(names))
	var cursor *string
	for range names {
		result, err := service.List(ctx, &gen.ListPayload{Limit: limit, Cursor: cursor})
		require.NoError(t, err)
		require.LessOrEqual(t, len(result.Items), limit)
		for _, agent := range result.Items {
			seen = append(seen, agent.Name)
		}
		cursor = result.NextCursor
		if cursor == nil {
			break
		}
	}
	require.Nil(t, cursor, "pagination must terminate")
	// Every agent exactly once, in the order the query promises.
	require.Equal(t, names, seen)
}

func TestListAgentsFillsAPageAcrossUnreadableAgents(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "caller")
	seedOrganizationUser(t, conn, "org-a", "other")
	// The readable agents sit behind a run of unreadable ones, so a single
	// LIMIT-sized read would return a page of nothing while rows remain.
	for _, name := range []string{"Hidden A", "Hidden B", "Hidden C", "Hidden D"} {
		createAgent(t, conn, "org-a", "other", name)
	}
	visible := createAgent(t, conn, "org-a", "caller", "Visible")
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-a", "caller")

	result, err := service.List(ctx, &gen.ListPayload{Limit: 2})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, visible.ID.String(), result.Items[0].ID)
	require.Nil(t, result.NextCursor)
}

func TestListAgentsPagesDescendingWithoutRepeatingOrSkipping(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "caller")
	names := []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo"}
	for _, name := range names {
		createAgent(t, conn, "org-a", "caller", name)
	}
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-a", "caller")

	seen := make([]string, 0, len(names))
	var cursor *string
	for range names {
		result, err := service.List(ctx, &gen.ListPayload{Limit: 2, NameOrder: "desc", Cursor: cursor})
		require.NoError(t, err)
		for _, agent := range result.Items {
			seen = append(seen, agent.Name)
		}
		cursor = result.NextCursor
		if cursor == nil {
			break
		}
	}
	require.Nil(t, cursor, "pagination must terminate")
	require.Equal(t, []string{"Echo", "Delta", "Charlie", "Bravo", "Alpha"}, seen)
}

func TestListAgentsSearchesByName(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "caller")
	for _, name := range []string{"Billing bot", "Deploy bot", "Triage helper", "billing sidecar"} {
		createAgent(t, conn, "org-a", "caller", name)
	}
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-a", "caller")

	// Case-insensitive, and it is the search that decides the page, not the
	// page that decides the search.
	query := "billing"
	result, err := service.List(ctx, &gen.ListPayload{Limit: 50, Search: &query})
	require.NoError(t, err)
	require.Len(t, result.Items, 2)
	require.Equal(t, "Billing bot", result.Items[0].Name)
	require.Equal(t, "billing sidecar", result.Items[1].Name)

	// A wildcard the caller typed is matched literally, not as a pattern.
	wildcard := "%"
	result, err = service.List(ctx, &gen.ListPayload{Limit: 50, Search: &wildcard})
	require.NoError(t, err)
	require.Empty(t, result.Items)
}

func TestListAgentsFiltersByLifecycleOwnerAndRegistration(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "caller")
	active := createAgent(t, conn, "org-a", "caller", "Active one")
	suspended := createAgent(t, conn, "org-a", "caller", "Suspended one")
	revoked := createAgent(t, conn, "org-a", "caller", "Revoked one")
	ctx := validatedHumanContext(t, "org-a", "caller")
	_, err := agentsrepo.New(conn).SuspendAgent(ctx, agentsrepo.SuspendAgentParams{
		OrganizationID: "org-a", ID: suspended.ID,
	})
	require.NoError(t, err)
	_, err = agentsrepo.New(conn).RevokeAgent(ctx, agentsrepo.RevokeAgentParams{
		OrganizationID: "org-a", ID: revoked.ID,
	})
	require.NoError(t, err)
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})

	// The filter reads the same two columns the badge is derived from.
	result, err := service.List(ctx, &gen.ListPayload{Limit: 50, Lifecycle: []string{"active"}})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, active.ID.String(), result.Items[0].ID)

	result, err = service.List(ctx, &gen.ListPayload{
		Limit:     50,
		Lifecycle: []string{"suspended", "revoked"},
	})
	require.NoError(t, err)
	require.Len(t, result.Items, 2)

	// An owner nobody matches empties the list rather than ignoring the filter.
	result, err = service.List(ctx, &gen.ListPayload{
		Limit:        50,
		OwnerUserIds: []string{"nobody"},
	})
	require.NoError(t, err)
	require.Empty(t, result.Items)

	result, err = service.List(ctx, &gen.ListPayload{
		Limit:        50,
		OwnerUserIds: []string{"caller"},
	})
	require.NoError(t, err)
	require.Len(t, result.Items, 3)

	// Everything was registered just now, so a window ending before now holds
	// none of it and a window starting before now holds all of it.
	past := time.Now().Add(-time.Hour).Format(time.RFC3339)
	result, err = service.List(ctx, &gen.ListPayload{Limit: 50, RegisteredBefore: &past})
	require.NoError(t, err)
	require.Empty(t, result.Items)

	result, err = service.List(ctx, &gen.ListPayload{Limit: 50, RegisteredAfter: &past})
	require.NoError(t, err)
	require.Len(t, result.Items, 3)

	bad := "not-a-moment"
	_, err = service.List(ctx, &gen.ListPayload{Limit: 50, RegisteredAfter: &bad})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestListAgentsRejectsATamperedCursor(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "caller")
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-a", "caller")

	for _, cursor := range []string{"not-base64!!", "YWJj", "QWxwaGF8bm90LWEtdXVpZA"} {
		_, err := service.List(ctx, &gen.ListPayload{Cursor: &cursor})
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
}

func TestListAgentsRejectsACursorThatIsNotUTF8(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "caller")
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-a", "caller")

	// Postgres rejects a text parameter that is not valid UTF-8, so without a
	// check this reaches the database and comes back as an unexpected error
	// rather than the bad request it is.
	cursor := base64.RawURLEncoding.EncodeToString(
		append([]byte{0xff, 0xfe}, []byte("|"+uuid.New().String())...),
	)
	_, err := service.List(ctx, &gen.ListPayload{Cursor: &cursor})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestListAgentsWalksPastRowsTheCallerMayNotRead(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "caller")
	seedOrganizationUser(t, conn, "org-a", "other")
	// A long run of unreadable agents ahead of a readable one. Each scan reads
	// limit+1 rows, so at limit 1 this takes many of them: the walk must keep
	// going rather than hand back a short page that reads as the end.
	for i := range 60 {
		createAgent(t, conn, "org-a", "other", fmt.Sprintf("Hidden %03d", i))
	}
	visible := createAgent(t, conn, "org-a", "caller", "Zzz visible")
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-a", "caller")

	result, err := service.List(ctx, &gen.ListPayload{Limit: 1})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, visible.ID.String(), result.Items[0].ID)
	require.Nil(t, result.NextCursor, "the readable agent is the last one")
}

func TestListAgentsCursorNamesOnlyAgentsTheCallerCanRead(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "caller")
	seedOrganizationUser(t, conn, "org-a", "other")
	// Readable rows either side of an unreadable one, so the page boundary
	// falls next to a hidden agent.
	createAgent(t, conn, "org-a", "caller", "Alpha visible")
	createAgent(t, conn, "org-a", "other", "Bravo hidden")
	createAgent(t, conn, "org-a", "caller", "Charlie visible")
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-a", "caller")

	result, err := service.List(ctx, &gen.ListPayload{Limit: 1})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.NotNil(t, result.NextCursor)

	// The cursor is reversible base64, so whatever it names is readable by
	// anyone holding it. It must never name an agent withheld from them.
	name, _, err := decodeAgentCursor(*result.NextCursor)
	require.NoError(t, err)
	require.Equal(t, "alpha visible", name)
	require.NotContains(t, name, "hidden")

	// And the walk still reaches what is past the hidden row.
	next, err := service.List(ctx, &gen.ListPayload{Limit: 1, Cursor: result.NextCursor})
	require.NoError(t, err)
	require.Len(t, next.Items, 1)
	require.Equal(t, "Charlie visible", next.Items[0].Name)
}

func TestListAgentsRejectsAnOutOfRangeLimit(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "caller")
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-a", "caller")

	_, err := service.List(ctx, &gen.ListPayload{Limit: maxAgentPageSize + 1})
	requireOopsCode(t, err, oops.CodeBadRequest)
	_, err = service.List(ctx, &gen.ListPayload{Limit: -1})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestListAgentsWrapsHumanLoadingFailure(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	var logs bytes.Buffer
	service.logger = slog.New(slog.NewTextHandler(&logs, nil))
	conn.Close()
	result, err := service.List(validatedHumanContext(t, "org-a", "caller"), &gen.ListPayload{})
	require.Nil(t, result)
	requireOopsCode(t, err, oops.CodeUnexpected)
	require.Contains(t, logs.String(), "list managed agents")
}
