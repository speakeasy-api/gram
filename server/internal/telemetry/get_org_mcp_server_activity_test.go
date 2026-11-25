package telemetry_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

func orgActivityByProject(result *gen.GetOrgMcpServerActivityResult) map[string]map[string]*gen.McpServerActivity {
	byProject := make(map[string]map[string]*gen.McpServerActivity, len(result.Projects))
	for _, project := range result.Projects {
		byProject[project.ProjectID] = mcpActivityByTarget(project.Activity)
	}
	return byProject
}

func TestGetOrgMcpServerActivity_ReadsEveryProjectInOneCall(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()

	other, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: "Other project", Slug: "other-" + uuid.NewString()[:8], OrganizationID: ti.orgID,
	})
	require.NoError(t, err)

	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID:   projectID,
		timestamp:   now.Add(-2 * time.Hour),
		toolsetSlug: "home-svc",
		toolName:    "charge",
		userEmail:   "alice@example.com",
		statusCode:  200,
	})
	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID:   other.ID.String(),
		timestamp:   now.Add(-3 * time.Hour),
		toolsetSlug: "other-svc",
		toolName:    "refund",
		userEmail:   "bob@example.com",
		statusCode:  200,
	})
	// Outside the one-day window: the org read never scans it.
	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID:   projectID,
		timestamp:   now.Add(-5 * 24 * time.Hour),
		toolsetSlug: "stale-svc",
		toolName:    "refund",
		userEmail:   "bob@example.com",
		statusCode:  200,
	})

	result, err := ti.service.GetOrgMcpServerActivity(ctx, &gen.GetOrgMcpServerActivityPayload{RecentWindowDays: 1})
	require.NoError(t, err, "cause: %v", errors.Unwrap(err))
	require.Equal(t, 1, result.RecentWindowDays)

	byProject := orgActivityByProject(result)
	require.Len(t, byProject, 2)

	home := byProject[projectID]["home-svc"]
	require.NotNil(t, home)
	require.Positive(t, home.RecentToolCalls)
	require.Equal(t, home.RecentToolCalls, home.TotalToolCalls)
	require.Nil(t, byProject[projectID]["stale-svc"])

	require.NotNil(t, byProject[other.ID.String()]["other-svc"])
	require.Positive(t, byProject[other.ID.String()]["other-svc"].RecentToolCalls)
}

func TestGetOrgMcpServerActivity_SkipsProjectsTheCallerCannotRead(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()

	hidden, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: "Hidden project", Slug: "hidden-" + uuid.NewString()[:8], OrganizationID: ti.orgID,
	})
	require.NoError(t, err)
	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID:   hidden.ID.String(),
		timestamp:   now.Add(-time.Hour),
		toolsetSlug: "hidden-svc",
		toolName:    "charge",
		userEmail:   "alice@example.com",
		statusCode:  200,
	})

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeProjectRead,
		Selector: authz.NewSelector(authz.ScopeProjectRead, projectID),
	})

	result, err := ti.service.GetOrgMcpServerActivity(ctx, &gen.GetOrgMcpServerActivityPayload{RecentWindowDays: 1})
	require.NoError(t, err, "cause: %v", errors.Unwrap(err))

	byProject := orgActivityByProject(result)
	require.Contains(t, byProject, projectID)
	require.NotContains(t, byProject, hidden.ID.String())
}
