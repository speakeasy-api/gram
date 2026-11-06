package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func withProjectGrants(t *testing.T, ctx context.Context, orgRead bool) context.Context {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	grants := []authz.Grant{{
		Scope:    authz.ScopeProjectRead,
		Selector: authz.NewSelector(authz.ScopeProjectRead, authCtx.ProjectID.String()),
	}}
	if orgRead {
		grants = append(grants, authz.Grant{
			Scope:    authz.ScopeOrgRead,
			Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
		})
	}
	return authztest.WithExactGrants(t, ctx, grants...)
}

func overviewWithUsage(t *testing.T, ctx context.Context, ti *testInstance, now time.Time, includeUsage bool) *gen.GetProjectOverviewResult {
	t.Helper()
	res, err := ti.service.GetProjectOverview(ctx, &gen.GetProjectOverviewPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		From:             now.Add(-time.Hour).Format(time.RFC3339),
		To:               now.Add(time.Hour).Format(time.RFC3339),
		IncludeUsage:     includeUsage,
	})
	require.NoError(t, err)
	require.NotNil(t, res.Summary)
	return res
}

func TestGetProjectOverview_UsageOmittedByDefault(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	ctx = withProjectGrants(t, ctx, true)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	now := time.Now().UTC()
	insertAttributeUsageLog(t, ctx, authCtx.ProjectID.String(), now.Add(-10*time.Minute), uuid.NewString(), 0.10, 5, "opus", "cursor", "a@x.com", "Engineering", nil)

	res := overviewWithUsage(t, ctx, ti, now, false)
	require.Nil(t, res.UsageByUser)
	require.Nil(t, res.UsageByAgent)
	require.Nil(t, res.McpUsage)
}

func TestGetProjectOverview_UsageAgentView(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	ctx = withProjectGrants(t, ctx, true)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()
	ts := now.Add(-10 * time.Minute)
	insertAttributeUsageLog(t, ctx, projectID, ts, uuid.NewString(), 0.10, 5, "opus", "cursor", "a@x.com", "Engineering", nil)
	insertAttributeUsageLog(t, ctx, projectID, ts, uuid.NewString(), 0.30, 50, "gpt-5.4-codex", "codex", "b@x.com", "Engineering", nil)

	res := overviewWithUsage(t, ctx, ti, now, true)

	require.NotNil(t, res.UsageByUser)
	require.Equal(t, "email", res.UsageByUser.GroupBy)
	require.ElementsMatch(t, []string{"a@x.com", "b@x.com"}, groupValues(res.UsageByUser.Table))
	require.Empty(t, res.UsageByUser.Timeseries, "home reads only the table")

	require.NotNil(t, res.UsageByAgent)
	require.Equal(t, "hook_source", res.UsageByAgent.GroupBy)
	require.ElementsMatch(t, []string{"cursor", "codex"}, groupValues(res.UsageByAgent.Table))

	require.Nil(t, res.McpUsage, "agent view never reads MCP usage")
}

func TestGetProjectOverview_UsageMCPViewWhenNoAgentUsage(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	ctx = withProjectGrants(t, ctx, true)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()
	ts := now.Add(-9 * time.Minute)
	userA := "ext-user-a-" + uuid.NewString()
	userB := "ext-user-b-" + uuid.NewString()
	const (
		search = "tools:http:api:search"
		create = "tools:http:api:create"
		remove = "tools:http:api:delete"
	)
	insertCall := func(urn string, status int32, externalUserID string) {
		insertToolCallLogWithUser(t, ctx, projectID, uuid.NewString(), ts, urn, status, 0.5, "", externalUserID)
	}

	// search: 4 calls, 1 failure (25%). create: 2 calls, 1 failure (50%).
	// delete: 1 call, no failures. userA makes 5 calls, userB 2.
	insertCall(search, 200, userA)
	insertCall(search, 200, userA)
	insertCall(search, 200, userB)
	insertCall(search, 500, userA)
	insertCall(create, 200, userA)
	insertCall(create, 404, userB)
	insertCall(remove, 200, userA)
	// No external user: never counted.
	insertCall(search, 500, "")

	res := overviewWithUsage(t, ctx, ti, now, true)

	require.NotNil(t, res.UsageByUser)
	require.Nil(t, res.UsageByAgent)
	require.Equal(t, &gen.ProjectMcpUsage{
		EndUsersCount: 2,
		TopEndUsers: []*gen.ProjectMcpEndUser{
			{UserID: userA, ToolCalls: 5},
			{UserID: userB, ToolCalls: 2},
		},
		TopTools: []*gen.ProjectMcpToolUsage{
			{Urn: search, Calls: 4, Failures: 1},
			{Urn: create, Calls: 2, Failures: 1},
			{Urn: remove, Calls: 1, Failures: 0},
		},
		TopFailingTools: []*gen.ProjectMcpToolUsage{
			{Urn: create, Calls: 2, Failures: 1},
			{Urn: search, Calls: 4, Failures: 1},
		},
	}, res.McpUsage)
}

func TestGetProjectOverview_UsageWithoutOrgReadFallsBackToMCPView(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	ctx = withProjectGrants(t, ctx, false)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	now := time.Now().UTC()
	insertAttributeUsageLog(t, ctx, authCtx.ProjectID.String(), now.Add(-10*time.Minute), uuid.NewString(), 0.10, 5, "opus", "cursor", "a@x.com", "Engineering", nil)

	res := overviewWithUsage(t, ctx, ti, now, true)

	require.Nil(t, res.UsageByUser, "agent usage needs org:read")
	require.Nil(t, res.UsageByAgent)
	require.NotNil(t, res.McpUsage, "MCP view section must settle")
}

func groupValues(rows []*gen.QueryRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.GroupValue)
	}
	return out
}
