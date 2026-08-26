package access

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpapprovalrepo "github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/policybypass"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	telemetryRepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func createShadowMCPProject(t *testing.T, ctx context.Context, ti *testInstance, organizationID string) projectsrepo.Project {
	t.Helper()

	projectSlug := uuid.NewString()
	project, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           projectSlug,
		Slug:           projectSlug,
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	return project
}

func TestService_ListShadowMCPInventory_ComposesInventoryUsageAndPolicyState(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	otherProject := createShadowMCPProject(t, ctx, ti, authCtx.ActiveOrganizationID)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	ch := telemetryRepo.New(ti.chConn)
	now := time.Now().UTC()
	require.NoError(t, ch.UpsertShadowMCPInventoryURLs(ctx, []telemetryRepo.UpsertShadowMCPInventoryURLParams{
		{
			GramProjectID:      projectID,
			CanonicalServerURL: "https://github.example.com/mcp",
			URLHost:            "github.example.com",
			ServerName:         "GitHub",
			SeenAt:             now.Add(-2 * time.Hour),
			FirstSeen:          now.Add(-2 * time.Hour),
			LastSeen:           now.Add(-2 * time.Hour),
			UpdatedAt:          now.Add(-2 * time.Hour),
		},
		{
			GramProjectID:      projectID,
			CanonicalServerURL: "https://mcp.speakeasy.com/mcp",
			URLHost:            "mcp.speakeasy.com",
			ServerName:         "Speakeasy",
			SeenAt:             now.Add(-1 * time.Hour),
			FirstSeen:          now.Add(-1 * time.Hour),
			LastSeen:           now.Add(-1 * time.Hour),
			UpdatedAt:          now.Add(-1 * time.Hour),
		},
		{
			GramProjectID:      otherProject.ID.String(),
			CanonicalServerURL: "https://other-project.example.com/mcp",
			URLHost:            "other-project.example.com",
			ServerName:         "Other Project",
			SeenAt:             now,
			FirstSeen:          now,
			LastSeen:           now,
			UpdatedAt:          now,
		},
	}))

	insertShadowMCPInventoryTelemetry(t, ctx, ti, shadowMCPInventoryTelemetryInput{
		ProjectID:  projectID,
		ServerURL:  "https://mcp.speakeasy.com/mcp?token=one",
		ServerName: "Speakeasy",
		UserEmail:  "alex@example.com",
		ObservedAt: now.Add(-30 * time.Minute),
	})
	insertShadowMCPInventoryTelemetry(t, ctx, ti, shadowMCPInventoryTelemetryInput{
		ProjectID:  projectID,
		ServerURL:  "https://mcp.speakeasy.com/mcp?token=two#ignored",
		ServerName: "Speakeasy",
		UserEmail:  "alex@example.com",
		ObservedAt: now.Add(-20 * time.Minute),
	})
	insertShadowMCPInventoryTelemetry(t, ctx, ti, shadowMCPInventoryTelemetryInput{
		ProjectID:  projectID,
		ServerURL:  "https://mcp.speakeasy.com/mcp",
		ServerName: "Speakeasy",
		UserEmail:  "sam@example.com",
		ObservedAt: now.Add(-10 * time.Minute),
	})

	blockPolicy := createShadowMCPInventoryPolicy(t, ctx, ti, shadowMCPInventoryPolicyInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
		Name:           "Block Shadow MCP",
		Action:         "block",
	})
	flagPolicy := createShadowMCPInventoryPolicy(t, ctx, ti, shadowMCPInventoryPolicyInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
		Name:           "Flag Shadow MCP",
		Action:         "flag",
	})
	grantShadowMCPInventoryBypass(t, ctx, ti, authCtx.ActiveOrganizationID, blockPolicy.ID.String(), "https://mcp.speakeasy.com/mcp")
	requestID := createShadowMCPInventoryBypassRequest(t, ctx, ti, shadowMCPInventoryBypassRequestInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
		PolicyID:       blockPolicy.ID.String(),
		ServerURL:      "https://github.example.com/mcp",
		RequesterID:    authCtx.UserID,
		RequesterEmail: "alex@example.com",
		RequestedAt:    now.Add(-5 * time.Minute),
	})
	_ = createShadowMCPInventoryBypassRequest(t, ctx, ti, shadowMCPInventoryBypassRequestInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
		PolicyID:       flagPolicy.ID.String(),
		ServerURL:      "https://github.example.com/mcp",
		RequesterID:    "user_flagged",
		RequesterEmail: "flagged@example.com",
		RequestedAt:    now.Add(-4 * time.Minute),
	})

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	result, err := ti.service.ListShadowMCPInventory(ctx, &gen.ListShadowMCPInventoryPayload{
		ProjectID: projectID,
		Limit:     10,
	})
	require.NoError(t, err)
	require.Len(t, result.Servers, 2)

	require.Nil(t, result.NextCursor)
	require.Len(t, result.Servers, 2)
	require.Nil(t, shadowMCPInventoryServerByURL(result.Servers, "https://other-project.example.com/mcp"))

	speakeasy := shadowMCPInventoryServerByURL(result.Servers, "https://mcp.speakeasy.com/mcp")
	require.NotNil(t, speakeasy)
	require.NotNil(t, speakeasy.ServerName)
	require.Equal(t, "Speakeasy", *speakeasy.ServerName)
	require.Equal(t, "mcp-speakeasy-com-mcp-b69171c9", speakeasy.ServerSlug)
	require.Equal(t, "mcp.speakeasy.com", speakeasy.URLHost)
	require.NotEmpty(t, speakeasy.FirstSeen)
	require.NotEmpty(t, speakeasy.LastSeen)
	require.NotNil(t, speakeasy.LastCalled)
	require.Equal(t, 3, speakeasy.ObservedUseCount)
	require.Equal(t, 2, speakeasy.UserCount)
	require.Equal(t, []string{"alex@example.com", "sam@example.com"}, speakeasy.TopUsers)
	require.Equal(t, shadowMCPInventoryAccessAllowed, speakeasy.Access)
	require.Equal(t, 0, speakeasy.RequestCount)
	require.Nil(t, speakeasy.LatestRequest)
	require.Equal(t, []string{blockPolicy.ID.String()}, speakeasy.AllowedPolicyIds)

	github := shadowMCPInventoryServerByURL(result.Servers, "https://github.example.com/mcp")
	require.NotNil(t, github)
	require.NotNil(t, github.ServerName)
	require.Equal(t, "GitHub", *github.ServerName)
	require.Equal(t, "github-example-com-mcp-d8860eea", github.ServerSlug)
	require.Nil(t, github.LastCalled)
	require.Equal(t, 0, github.ObservedUseCount)
	require.Equal(t, 0, github.UserCount)
	require.Empty(t, github.TopUsers)
	require.Equal(t, shadowMCPInventoryAccessBlocked, github.Access)
	require.Equal(t, 1, github.RequestCount)
	require.NotNil(t, github.LatestRequest)
	require.Equal(t, requestID, github.LatestRequest.ID)
	require.Equal(t, blockPolicy.ID.String(), github.LatestRequest.PolicyID)
	require.Equal(t, "alex@example.com", github.LatestRequest.RequesterEmail)
	require.Empty(t, github.AllowedPolicyIds)
}

func TestService_ListShadowMCPInventory_CursorPagination(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	ch := telemetryRepo.New(ti.chConn)
	now := time.Now().UTC()
	for i, url := range []string{
		"https://one.example.com/mcp",
		"https://two.example.com/mcp",
		"https://three.example.com/mcp",
	} {
		require.NoError(t, ch.UpsertShadowMCPInventoryURLs(ctx, []telemetryRepo.UpsertShadowMCPInventoryURLParams{
			{
				GramProjectID:      projectID,
				CanonicalServerURL: url,
				URLHost:            strings.TrimPrefix(strings.TrimSuffix(url, "/mcp"), "https://"),
				ServerName:         url,
				SeenAt:             now.Add(time.Duration(i) * time.Minute),
				FirstSeen:          now.Add(time.Duration(i) * time.Minute),
				LastSeen:           now.Add(time.Duration(i) * time.Minute),
				UpdatedAt:          now.Add(time.Duration(i) * time.Minute),
			},
		}))
	}

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	firstPage, err := ti.service.ListShadowMCPInventory(ctx, &gen.ListShadowMCPInventoryPayload{
		ProjectID: projectID,
		Limit:     2,
	})
	require.NoError(t, err)
	require.Len(t, firstPage.Servers, 2)
	require.NotNil(t, firstPage.NextCursor)

	secondPage, err := ti.service.ListShadowMCPInventory(ctx, &gen.ListShadowMCPInventoryPayload{
		ProjectID: projectID,
		Limit:     2,
		Cursor:    firstPage.NextCursor,
	})
	require.NoError(t, err)
	require.Len(t, secondPage.Servers, 1)
	require.Nil(t, secondPage.NextCursor)
}

func TestService_ListShadowMCPInventory_ServerNameIsOptional(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	now := time.Now().UTC()
	require.NoError(t, telemetryRepo.New(ti.chConn).UpsertShadowMCPInventoryURLs(ctx, []telemetryRepo.UpsertShadowMCPInventoryURLParams{
		{
			GramProjectID:      projectID,
			CanonicalServerURL: "https://unnamed.example.com/mcp",
			URLHost:            "unnamed.example.com",
			ServerName:         "",
			SeenAt:             now,
			FirstSeen:          now,
			LastSeen:           now,
			UpdatedAt:          now,
		},
	}))

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	result, err := ti.service.ListShadowMCPInventory(ctx, &gen.ListShadowMCPInventoryPayload{
		ProjectID: projectID,
		Limit:     10,
	})
	require.NoError(t, err)
	require.Len(t, result.Servers, 1)

	require.Nil(t, result.Servers[0].ServerName)
}

func TestService_GetShadowMCPInventoryServer_ComposesOneURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	now := time.Now().UTC()

	require.NoError(t, telemetryRepo.New(ti.chConn).UpsertShadowMCPInventoryURLs(ctx, []telemetryRepo.UpsertShadowMCPInventoryURLParams{
		{
			GramProjectID:      projectID,
			CanonicalServerURL: "https://detail.example.com/mcp",
			URLHost:            "detail.example.com",
			ServerName:         "Detail MCP",
			SeenAt:             now.Add(-2 * time.Hour),
			FirstSeen:          now.Add(-2 * time.Hour),
			LastSeen:           now.Add(-30 * time.Minute),
			UpdatedAt:          now.Add(-30 * time.Minute),
		},
	}))
	insertShadowMCPInventoryTelemetry(t, ctx, ti, shadowMCPInventoryTelemetryInput{
		ProjectID:  projectID,
		ServerURL:  "https://detail.example.com/mcp?token=ignored",
		ServerName: "Detail MCP",
		UserEmail:  "alex@example.com",
		ObservedAt: now.Add(-10 * time.Minute),
	})
	policy := createShadowMCPInventoryPolicy(t, ctx, ti, shadowMCPInventoryPolicyInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
		Name:           "Block Shadow MCP",
		Action:         "block",
	})
	grantShadowMCPInventoryBypass(t, ctx, ti, authCtx.ActiveOrganizationID, policy.ID.String(), "https://detail.example.com/mcp")

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	server, err := ti.service.GetShadowMCPInventoryServer(ctx, &gen.GetShadowMCPInventoryServerPayload{
		ProjectID:  projectID,
		ServerSlug: "detail-example-com-mcp-30d7c46c",
	})
	require.NoError(t, err)
	require.NotNil(t, server.LastCalled)

	require.Equal(t, "https://detail.example.com/mcp", server.CanonicalServerURL)
	require.Equal(t, "detail-example-com-mcp-30d7c46c", server.ServerSlug)
	require.NotNil(t, server.ServerName)
	require.Equal(t, "Detail MCP", *server.ServerName)
	require.Equal(t, "detail.example.com", server.URLHost)
	require.Equal(t, shadowMCPInventoryAccessAllowed, server.Access)
	require.Equal(t, []string{policy.ID.String()}, server.AllowedPolicyIds)
	require.Equal(t, 1, server.ObservedUseCount)
	require.Equal(t, 1, server.UserCount)
	require.Equal(t, []string{"alex@example.com"}, server.TopUsers)
}

func TestService_UpdateShadowMCPInventoryServerName_TrimsAndSavesOverride(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	seedShadowMCPInventoryServer(t, ctx, ti, projectID, "GitHub MCP")

	err := ti.service.UpdateShadowMCPInventoryServerName(ctx, &gen.UpdateShadowMCPInventoryServerNamePayload{
		ProjectID: projectID,
		ServerURL: "https://github.example.com/mcp?ignored=true",
		Name:      "  Engineering GitHub  ",
	})
	require.NoError(t, err)
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	server, err := ti.service.GetShadowMCPInventoryServer(ctx, &gen.GetShadowMCPInventoryServerPayload{
		ProjectID:  projectID,
		ServerSlug: "github-example-com-mcp-d8860eea",
	})
	require.NoError(t, err)
	require.NotNil(t, server.ServerName)
	require.Equal(t, "Engineering GitHub", *server.ServerName)
}

func TestService_UpdateShadowMCPInventoryServerName_PreservesOverrideAfterLaterObservation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	seedShadowMCPInventoryServer(t, ctx, ti, projectID, "GitHub MCP")

	err := ti.service.UpdateShadowMCPInventoryServerName(ctx, &gen.UpdateShadowMCPInventoryServerNamePayload{
		ProjectID: projectID,
		ServerURL: "https://github.example.com/mcp",
		Name:      "Engineering GitHub",
	})
	require.NoError(t, err)

	now := time.Now().UTC()
	require.NoError(t, telemetryRepo.New(ti.chConn).UpsertShadowMCPInventoryURLs(ctx, []telemetryRepo.UpsertShadowMCPInventoryURLParams{{
		GramProjectID:      projectID,
		CanonicalServerURL: "https://github.example.com/mcp",
		URLHost:            "github.example.com",
		ServerName:         "GitHub Enterprise MCP",
		SeenAt:             now,
		FirstSeen:          now,
		LastSeen:           now,
		UpdatedAt:          now,
	}}))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)
	row, err := telemetryRepo.New(ti.chConn).GetShadowMCPInventoryURL(ctx, telemetryRepo.GetShadowMCPInventoryURLParams{
		GramProjectID:      projectID,
		CanonicalServerURL: "https://github.example.com/mcp",
	})
	require.NoError(t, err)
	require.NotNil(t, row)
	require.Equal(t, "GitHub Enterprise MCP", row.ServerName)
	require.Equal(t, "Engineering GitHub", row.ServerNameOverride)

	server, err := ti.service.GetShadowMCPInventoryServer(ctx, &gen.GetShadowMCPInventoryServerPayload{
		ProjectID:  projectID,
		ServerSlug: "github-example-com-mcp-d8860eea",
	})
	require.NoError(t, err)
	require.NotNil(t, server.ServerName)
	require.Equal(t, "Engineering GitHub", *server.ServerName)
}

func TestService_UpdateShadowMCPInventoryServerName_ClearsOverrideAndFallsBackToLatestObservedName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	seedShadowMCPInventoryServer(t, ctx, ti, projectID, "GitHub MCP")

	err := ti.service.UpdateShadowMCPInventoryServerName(ctx, &gen.UpdateShadowMCPInventoryServerNamePayload{
		ProjectID: projectID,
		ServerURL: "https://github.example.com/mcp",
		Name:      "Engineering GitHub",
	})
	require.NoError(t, err)
	now := time.Now().UTC()
	require.NoError(t, telemetryRepo.New(ti.chConn).UpsertShadowMCPInventoryURLs(ctx, []telemetryRepo.UpsertShadowMCPInventoryURLParams{{
		GramProjectID:      projectID,
		CanonicalServerURL: "https://github.example.com/mcp",
		URLHost:            "github.example.com",
		ServerName:         "GitHub Enterprise MCP",
		SeenAt:             now,
		FirstSeen:          now,
		LastSeen:           now,
		UpdatedAt:          now,
	}}))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)
	err = ti.service.UpdateShadowMCPInventoryServerName(ctx, &gen.UpdateShadowMCPInventoryServerNamePayload{
		ProjectID: projectID,
		ServerURL: "https://github.example.com/mcp",
		Name:      "   ",
	})
	require.NoError(t, err)
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	server, err := ti.service.GetShadowMCPInventoryServer(ctx, &gen.GetShadowMCPInventoryServerPayload{
		ProjectID:  projectID,
		ServerSlug: "github-example-com-mcp-d8860eea",
	})
	require.NoError(t, err)
	require.NotNil(t, server.ServerName)
	require.Equal(t, "GitHub Enterprise MCP", *server.ServerName)
}

func TestService_UpdateShadowMCPInventoryServerName_RequiresOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgRead, Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID)})

	err := ti.service.UpdateShadowMCPInventoryServerName(ctx, &gen.UpdateShadowMCPInventoryServerNamePayload{
		ProjectID: authCtx.ProjectID.String(),
		ServerURL: "https://github.example.com/mcp",
		Name:      "Engineering GitHub",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestService_UpdateShadowMCPInventoryServerName_ReturnsNotFoundForUnknownURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	err := ti.service.UpdateShadowMCPInventoryServerName(ctx, &gen.UpdateShadowMCPInventoryServerNamePayload{
		ProjectID: authCtx.ProjectID.String(),
		ServerURL: "https://unknown.example.com/mcp?ignored=true",
		Name:      "Unknown MCP",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestService_UpdateShadowMCPInventoryServerName_RejectsProjectFromAnotherOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	otherOrganizationID := uuid.NewString()
	otherProject := createShadowMCPProject(t, ctx, ti, otherOrganizationID)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	seedShadowMCPInventoryServer(t, ctx, ti, otherProject.ID.String(), "Other Organization MCP")

	err := ti.service.UpdateShadowMCPInventoryServerName(ctx, &gen.UpdateShadowMCPInventoryServerNamePayload{
		ProjectID: otherProject.ID.String(),
		ServerURL: "https://github.example.com/mcp",
		Name:      "Renamed Across Organizations",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)

	row, err := telemetryRepo.New(ti.chConn).GetShadowMCPInventoryURL(ctx, telemetryRepo.GetShadowMCPInventoryURLParams{
		GramProjectID:      otherProject.ID.String(),
		CanonicalServerURL: "https://github.example.com/mcp",
	})
	require.NoError(t, err)
	require.NotNil(t, row)
	require.Equal(t, "Other Organization MCP", row.ServerName)
	require.Empty(t, row.ServerNameOverride)
}

func TestShadowMCPInventorySlugHash_ReturnsHashSuffix(t *testing.T) {
	t.Parallel()

	require.Equal(t, "30d7c46c", shadowMCPInventorySlugHash("detail-example-com-mcp-30d7c46c"))
}

func TestShadowMCPInventorySlugHash_RejectsInvalidSuffix(t *testing.T) {
	t.Parallel()

	require.Empty(t, shadowMCPInventorySlugHash("detail-example-com-mcp-not-hash"))
	require.Empty(t, shadowMCPInventorySlugHash("detail-example-com-mcp-30D7C46C"))
	require.Empty(t, shadowMCPInventorySlugHash("30d7c46c"))
}

func TestService_GetShadowMCPInventoryServer_RejectsMismatchedReadableSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	seenAt := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)

	require.NoError(t, telemetryRepo.New(ti.chConn).UpsertShadowMCPInventoryURLs(ctx, []telemetryRepo.UpsertShadowMCPInventoryURLParams{
		{
			GramProjectID:      projectID,
			CanonicalServerURL: "https://detail.example.com/mcp",
			URLHost:            "detail.example.com",
			ServerName:         "Detail MCP",
			SeenAt:             seenAt,
		},
	}))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	_, err := ti.service.GetShadowMCPInventoryServer(ctx, &gen.GetShadowMCPInventoryServerPayload{
		ProjectID:  projectID,
		ServerSlug: "wrong-readable-prefix-30d7c46c",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestService_ListShadowMCPInventory_RequiresOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgRead, Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID)})

	_, err := ti.service.ListShadowMCPInventory(ctx, &gen.ListShadowMCPInventoryPayload{
		ProjectID: authCtx.ProjectID.String(),
		Limit:     10,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestService_ListShadowMCPInventory_BackendFailureIsUnexpected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	require.NoError(t, ti.chConn.Close())

	_, err := ti.service.ListShadowMCPInventory(ctx, &gen.ListShadowMCPInventoryPayload{
		ProjectID: authCtx.ProjectID.String(),
		Limit:     10,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnexpected, oopsErr.Code)
}

func TestService_ListShadowMCPInventory_InvalidCursorIsBadRequest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	cursor := "not-a-valid-cursor"

	_, err := ti.service.ListShadowMCPInventory(ctx, &gen.ListShadowMCPInventoryPayload{
		ProjectID: authCtx.ProjectID.String(),
		Limit:     10,
		Cursor:    &cursor,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestService_ListShadowMCPInventoryUsers_ReturnsPaginatedUsersForCanonicalURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	now := time.Now().UTC()

	insertShadowMCPInventoryTelemetry(t, ctx, ti, shadowMCPInventoryTelemetryInput{
		ProjectID:  projectID,
		ServerURL:  "https://mcp.speakeasy.com/mcp?token=one",
		ServerName: "Speakeasy",
		UserEmail:  "ada@example.com",
		HookSource: "claude-code",
		ObservedAt: now.Add(-30 * time.Minute),
	})
	insertShadowMCPInventoryTelemetry(t, ctx, ti, shadowMCPInventoryTelemetryInput{
		ProjectID:  projectID,
		ServerURL:  "https://mcp.speakeasy.com/mcp?token=two#ignored",
		ServerName: "Speakeasy",
		UserEmail:  "ada@example.com",
		HookSource: "claude-code",
		ObservedAt: now.Add(-20 * time.Minute),
	})
	insertShadowMCPInventoryTelemetry(t, ctx, ti, shadowMCPInventoryTelemetryInput{
		ProjectID:  projectID,
		ServerURL:  "https://mcp.speakeasy.com/mcp",
		ServerName: "Speakeasy",
		UserEmail:  "ada@example.com",
		HookSource: "cursor",
		ObservedAt: now.Add(-10 * time.Minute),
	})
	insertShadowMCPInventoryTelemetry(t, ctx, ti, shadowMCPInventoryTelemetryInput{
		ProjectID:  projectID,
		ServerURL:  "https://mcp.speakeasy.com/mcp",
		ServerName: "Speakeasy",
		UserEmail:  "ada@example.com",
		HookSource: "",
		ObservedAt: now.Add(-5 * time.Minute),
	})
	insertShadowMCPInventoryTelemetry(t, ctx, ti, shadowMCPInventoryTelemetryInput{
		ProjectID:  projectID,
		ServerURL:  "https://mcp.speakeasy.com/mcp",
		ServerName: "Speakeasy",
		UserID:     "grace",
		HookSource: "cursor",
		ObservedAt: now.Add(-15 * time.Minute),
	})

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	firstPage, err := ti.service.ListShadowMCPInventoryUsers(ctx, &gen.ListShadowMCPInventoryUsersPayload{
		ProjectID: projectID,
		ServerURL: "https://mcp.speakeasy.com/mcp?token=ignored",
		Limit:     1,
	})
	require.NoError(t, err)
	require.Len(t, firstPage.Users, 1)
	require.Equal(t, 4, firstPage.Users[0].ObservedUseCount)

	require.NotNil(t, firstPage.NextCursor)
	require.Equal(t, "ada@example.com", firstPage.Users[0].UserKey)
	require.Nil(t, firstPage.Users[0].Name)
	require.NotNil(t, firstPage.Users[0].Email)
	require.Equal(t, "ada@example.com", *firstPage.Users[0].Email)
	require.Equal(t, formatTimeValue(now.Add(-5*time.Minute)), firstPage.Users[0].LastCalled)
	require.Equal(t, []*gen.ShadowMCPInventoryUserSource{
		{Source: "claude-code", ObservedUseCount: 2},
		{Source: "", ObservedUseCount: 1},
		{Source: "cursor", ObservedUseCount: 1},
	}, firstPage.Users[0].Sources)

	secondPage, err := ti.service.ListShadowMCPInventoryUsers(ctx, &gen.ListShadowMCPInventoryUsersPayload{
		ProjectID: projectID,
		ServerURL: "https://mcp.speakeasy.com/mcp",
		Limit:     1,
		Cursor:    firstPage.NextCursor,
	})
	require.NoError(t, err)
	require.Len(t, secondPage.Users, 1)
	require.Nil(t, secondPage.NextCursor)
	require.Equal(t, "grace", secondPage.Users[0].UserKey)
	require.Nil(t, secondPage.Users[0].Email)
	require.Equal(t, 1, secondPage.Users[0].ObservedUseCount)
	require.Equal(t, []*gen.ShadowMCPInventoryUserSource{{
		Source:           "cursor",
		ObservedUseCount: 1,
	}}, secondPage.Users[0].Sources)
}

func TestService_ListShadowMCPInventoryUsers_EmptyUsageIsValid(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	result, err := ti.service.ListShadowMCPInventoryUsers(ctx, &gen.ListShadowMCPInventoryUsersPayload{
		ProjectID: authCtx.ProjectID.String(),
		ServerURL: "https://unused.example.com/mcp",
		Limit:     10,
	})
	require.NoError(t, err)
	require.Empty(t, result.Users)
	require.Nil(t, result.NextCursor)
}

func TestService_ListShadowMCPInventoryUsers_InvalidURLIsBadRequest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	_, err := ti.service.ListShadowMCPInventoryUsers(ctx, &gen.ListShadowMCPInventoryUsersPayload{
		ProjectID: authCtx.ProjectID.String(),
		ServerURL: "stdio-server",
		Limit:     10,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestService_ResolveShadowMCPInventoryRequest_ApprovesURLAndResolvesPendingRequests(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	policyOne := createShadowMCPInventoryPolicy(t, ctx, ti, shadowMCPInventoryPolicyInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
		Name:           "Block Shadow MCP One",
		Action:         "block",
	})
	policyTwo := createShadowMCPInventoryPolicy(t, ctx, ti, shadowMCPInventoryPolicyInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
		Name:           "Block Shadow MCP Two",
		Action:         "block",
	})
	grantShadowMCPInventoryPolicyAudience(t, ctx, ti, authCtx.ActiveOrganizationID, policyOne.ID.String(), authz.AllUsersPrincipal())
	grantShadowMCPInventoryPolicyAudience(t, ctx, ti, authCtx.ActiveOrganizationID, policyTwo.ID.String(), authz.AllUsersPrincipal())
	firstRequestID := createShadowMCPInventoryBypassRequest(t, ctx, ti, shadowMCPInventoryBypassRequestInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
		PolicyID:       policyOne.ID.String(),
		ServerURL:      "https://mcp.example.com/mcp",
		RequesterID:    "user_one",
		RequesterEmail: "one@example.com",
		RequestedAt:    time.Now().Add(-2 * time.Minute),
	})
	secondRequestID := createShadowMCPInventoryBypassRequest(t, ctx, ti, shadowMCPInventoryBypassRequestInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
		PolicyID:       policyTwo.ID.String(),
		ServerURL:      "https://mcp.example.com/mcp",
		RequesterID:    "user_two",
		RequesterEmail: "two@example.com",
		RequestedAt:    time.Now().Add(-1 * time.Minute),
	})

	result, err := ti.service.ResolveShadowMCPInventoryRequest(ctx, &gen.ResolveShadowMCPInventoryRequestPayload{
		ProjectID: projectID,
		ServerURL: "https://mcp.example.com/mcp?token=ignored",
		Decision:  "allow",
		PolicyIds: []string{policyOne.ID.String(), policyTwo.ID.String()},
	})
	require.NoError(t, err)
	require.Equal(t, shadowMCPInventoryAccessAllowed, result.Access)
	require.Equal(t, 0, result.RequestCount)
	wantPolicyIDs := []string{policyOne.ID.String(), policyTwo.ID.String()}
	slices.Sort(wantPolicyIDs)
	require.Equal(t, wantPolicyIDs, result.AllowedPolicyIds)

	require.Equal(t, "approved", shadowMCPInventoryBypassRequestStatus(t, ctx, ti, projectID, firstRequestID))
	require.Equal(t, "approved", shadowMCPInventoryBypassRequestStatus(t, ctx, ti, projectID, secondRequestID))
	require.Equal(t, []string{authz.AllUsersPrincipal().String()}, shadowMCPInventoryBypassGrantPrincipals(t, ctx, ti, authCtx.ActiveOrganizationID, policyOne.ID.String(), "https://mcp.example.com/mcp"))
	require.Equal(t, []string{authz.AllUsersPrincipal().String()}, shadowMCPInventoryBypassGrantPrincipals(t, ctx, ti, authCtx.ActiveOrganizationID, policyTwo.ID.String(), "https://mcp.example.com/mcp"))
}

func TestService_ResolveShadowMCPInventoryRequest_DeniesURLAndResolvesPendingRequests(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	policy := createShadowMCPInventoryPolicy(t, ctx, ti, shadowMCPInventoryPolicyInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
		Name:           "Block Shadow MCP",
		Action:         "block",
	})
	grantShadowMCPInventoryPolicyAudience(t, ctx, ti, authCtx.ActiveOrganizationID, policy.ID.String(), authz.AllUsersPrincipal())
	requestID := createShadowMCPInventoryBypassRequest(t, ctx, ti, shadowMCPInventoryBypassRequestInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
		PolicyID:       policy.ID.String(),
		ServerURL:      "https://mcp.example.com/mcp",
		RequesterID:    "user_one",
		RequesterEmail: "one@example.com",
		RequestedAt:    time.Now(),
	})

	result, err := ti.service.ResolveShadowMCPInventoryRequest(ctx, &gen.ResolveShadowMCPInventoryRequestPayload{
		ProjectID: projectID,
		ServerURL: "https://mcp.example.com/mcp",
		Decision:  "deny",
	})
	require.NoError(t, err)
	require.Equal(t, shadowMCPInventoryAccessBlocked, result.Access)
	require.Equal(t, 0, result.RequestCount)
	require.Empty(t, result.AllowedPolicyIds)

	require.Equal(t, "denied", shadowMCPInventoryBypassRequestStatus(t, ctx, ti, projectID, requestID))
	require.Empty(t, shadowMCPInventoryBypassGrantPrincipals(t, ctx, ti, authCtx.ActiveOrganizationID, policy.ID.String(), "https://mcp.example.com/mcp"))
}

type shadowMCPInventoryTelemetryInput struct {
	ProjectID  string
	ServerURL  string
	ServerName string
	UserEmail  string
	UserID     string
	HookSource string
	ObservedAt time.Time
}

type shadowMCPInventoryPolicyInput struct {
	OrganizationID string
	ProjectID      string
	Name           string
	Action         string
	Disposition    string
	BlockedURLs    []string
	// AudienceType is the risk_policies.audience_type value; empty defaults to
	// "everyone". Set "targeted" to model a policy scoped to a subset of users.
	AudienceType string
}

type shadowMCPInventoryBypassRequestInput struct {
	OrganizationID string
	ProjectID      string
	PolicyID       string
	ServerURL      string
	RequesterID    string
	RequesterEmail string
	RequestedAt    time.Time
}

func createShadowMCPInventoryPolicy(t *testing.T, ctx context.Context, ti *testInstance, input shadowMCPInventoryPolicyInput) riskrepo.RiskPolicy {
	t.Helper()

	return createShadowMCPInventoryPolicyWithEnabled(t, ctx, ti, input, true)
}

func createShadowMCPInventoryPolicyWithEnabled(t *testing.T, ctx context.Context, ti *testInstance, input shadowMCPInventoryPolicyInput, enabled bool) riskrepo.RiskPolicy {
	t.Helper()

	projectID, err := uuid.Parse(input.ProjectID)
	require.NoError(t, err)

	audienceType := input.AudienceType
	if audienceType == "" {
		audienceType = "everyone"
	}

	policy, err := riskrepo.New(ti.conn).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
		ID:                   uuid.New(),
		ProjectID:            projectID,
		OrganizationID:       input.OrganizationID,
		Name:                 input.Name,
		Sources:              []string{"shadow_mcp"},
		Enabled:              enabled,
		Action:               input.Action,
		AudienceType:         audienceType,
		ShadowMcpDisposition: conv.ToPGTextEmpty(input.Disposition),
		AutoName:             false,
	})
	require.NoError(t, err)

	if len(input.BlockedURLs) > 0 {
		require.NoError(t, policybypass.ReconcilePolicyURLs(ctx, ti.conn, policybypass.ReconcilePolicyURLsInput{
			OrganizationID: input.OrganizationID,
			PolicyID:       policy.ID.String(),
			Scope:          authz.ScopeRiskPolicyBlock,
			DesiredURLs:    input.BlockedURLs,
			Principals:     []urn.Principal{authz.AllUsersPrincipal()},
		}))
	}

	return policy
}

func grantShadowMCPInventoryBypass(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, policyID string, serverURL string) {
	t.Helper()

	selector := authz.NewSelector(authz.ScopeRiskPolicyBypass, policyID)
	selector[authz.SelectorKeyServerURL] = serverURL
	require.NoError(t, authz.GrantResourceToPrincipals(ctx, ti.conn, authz.ResourceGrant{
		Resource: authz.Resource{
			OrganizationID: organizationID,
			Scope:          authz.ScopeRiskPolicyBypass,
			ResourceID:     policyID,
		},
		Principals: []urn.Principal{authz.AllUsersPrincipal()},
		Selector:   selector,
	}))
}

func grantShadowMCPInventoryBypassForPrincipals(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, policyID string, serverURL string, principals ...urn.Principal) {
	t.Helper()

	selector := authz.NewSelector(authz.ScopeRiskPolicyBypass, policyID)
	selector[authz.SelectorKeyServerURL] = serverURL
	require.NoError(t, authz.GrantResourceToPrincipals(ctx, ti.conn, authz.ResourceGrant{
		Resource: authz.Resource{
			OrganizationID: organizationID,
			Scope:          authz.ScopeRiskPolicyBypass,
			ResourceID:     policyID,
		},
		Principals: principals,
		Selector:   selector,
	}))
}

func grantShadowMCPInventoryPolicyAudience(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, policyID string, principals ...urn.Principal) {
	t.Helper()

	require.NoError(t, authz.ReplaceGrantAudience(ctx, ti.conn, authz.ResourceGrant{
		Resource: authz.Resource{
			OrganizationID: organizationID,
			Scope:          authz.ScopeRiskPolicyEvaluate,
			ResourceID:     policyID,
		},
		Principals: principals,
		Selector:   authz.NewSelector(authz.ScopeRiskPolicyEvaluate, policyID),
	}))
}

func shadowMCPInventoryBypassGrantPrincipals(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, policyID string, serverURL string) []string {
	t.Helper()

	grants, err := authz.ListGrantsForResource(ctx, ti.conn, authz.Resource{
		OrganizationID: organizationID,
		Scope:          authz.ScopeRiskPolicyBypass,
		ResourceID:     policyID,
	})
	require.NoError(t, err)

	principals := make([]string, 0, len(grants))
	for _, grant := range grants {
		if grant.Selector[authz.SelectorKeyServerURL] != serverURL {
			continue
		}
		principals = append(principals, grant.PrincipalUrn)
	}
	slices.Sort(principals)
	return slices.Compact(principals)
}

func shadowMCPInventoryBypassRequestStatus(t *testing.T, ctx context.Context, ti *testInstance, projectID string, requestID string) string {
	t.Helper()

	parsedProjectID, err := uuid.Parse(projectID)
	require.NoError(t, err)
	parsedRequestID, err := uuid.Parse(requestID)
	require.NoError(t, err)

	request, err := riskrepo.New(ti.conn).GetRiskPolicyBypassRequest(ctx, riskrepo.GetRiskPolicyBypassRequestParams{
		ID:        parsedRequestID,
		ProjectID: parsedProjectID,
	})
	require.NoError(t, err)
	return request.Status
}

func createShadowMCPInventoryBypassRequest(t *testing.T, ctx context.Context, ti *testInstance, input shadowMCPInventoryBypassRequestInput) string {
	t.Helper()

	projectID, err := uuid.Parse(input.ProjectID)
	require.NoError(t, err)
	policyID, err := uuid.Parse(input.PolicyID)
	require.NoError(t, err)
	dimensions, err := json.Marshal(map[string]string{authz.SelectorKeyServerURL: input.ServerURL})
	require.NoError(t, err)
	requestedAt := input.RequestedAt
	if requestedAt.IsZero() {
		requestedAt = time.Now()
	}

	requestID := uuid.New()
	request, err := riskrepo.New(ti.conn).UpsertRiskPolicyBypassRequest(ctx, riskrepo.UpsertRiskPolicyBypassRequestParams{
		ID:               requestID,
		OrganizationID:   input.OrganizationID,
		ProjectID:        projectID,
		RiskPolicyID:     policyID,
		TargetKind:       conv.ToPGText("shadow_mcp_server"),
		TargetLabel:      conv.ToPGText(input.ServerURL),
		TargetKey:        conv.ToPGText(input.ServerURL),
		TargetDimensions: dimensions,
		RequesterUserID:  input.RequesterID,
		RequesterEmail:   conv.ToPGText(input.RequesterEmail),
		Note:             conv.PtrToPGText(nil),
		Status:           "requested",
	})
	require.NoError(t, err)

	err = testrepo.New(ti.conn).UpdateRiskPolicyBypassRequestTimestamps(ctx, testrepo.UpdateRiskPolicyBypassRequestTimestampsParams{
		RequestedAt: conv.ToPGTimestamptz(requestedAt),
		ID:          request.ID,
		ProjectID:   projectID,
	})
	require.NoError(t, err)

	return request.ID.String()
}

func insertShadowMCPInventoryTelemetry(t *testing.T, ctx context.Context, ti *testInstance, input shadowMCPInventoryTelemetryInput) {
	t.Helper()

	attrs := map[string]any{
		"gram.event.source":     "hook",
		"gram.hook.source":      input.HookSource,
		"gram.mcp.server_url":   input.ServerURL,
		"gram.tool_call.source": input.ServerName,
		"gram.tool.name":        "mcp__speakeasy__search",
		"user.email":            input.UserEmail,
		"user.id":               input.UserID,
	}
	attrsJSON, err := json.Marshal(attrs)
	require.NoError(t, err)

	spanID := uuid.New().String()[:16]
	traceID := strings.ReplaceAll(uuid.NewString(), "-", "")
	err = telemetryRepo.New(ti.chConn).InsertTelemetryLog(ctx, telemetryRepo.InsertTelemetryLogParams{
		ID:                   uuid.NewString(),
		TimeUnixNano:         input.ObservedAt.UnixNano(),
		ObservedTimeUnixNano: input.ObservedAt.UnixNano(),
		SeverityText:         nil,
		Body:                 "shadow mcp inventory api call",
		TraceID:              &traceID,
		SpanID:               &spanID,
		Attributes:           string(attrsJSON),
		ResourceAttributes:   "{}",
		GramProjectID:        input.ProjectID,
		GramDeploymentID:     nil,
		GramFunctionID:       nil,
		GramURN:              "hooks:mcp__speakeasy__search",
		ServiceName:          "gram-hooks",
		ServiceVersion:       nil,
		GramChatID:           nil,
	})
	require.NoError(t, err)
}

func seedShadowMCPInventoryServer(t *testing.T, ctx context.Context, ti *testInstance, projectID string, serverName string) {
	t.Helper()

	now := time.Now().UTC().Add(-time.Minute)
	require.NoError(t, telemetryRepo.New(ti.chConn).UpsertShadowMCPInventoryURLs(ctx, []telemetryRepo.UpsertShadowMCPInventoryURLParams{{
		GramProjectID:      projectID,
		CanonicalServerURL: "https://github.example.com/mcp",
		URLHost:            "github.example.com",
		ServerName:         serverName,
		SeenAt:             now,
		FirstSeen:          now,
		LastSeen:           now,
		UpdatedAt:          now,
	}}))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)
}

func shadowMCPInventoryServerByURL(servers []*gen.ShadowMCPInventoryServer, canonicalURL string) *gen.ShadowMCPInventoryServer {
	for _, server := range servers {
		if server.CanonicalServerURL == canonicalURL {
			return server
		}
	}
	return nil
}

func TestService_ListShadowMCPInventory_AllowAllDispositionUsesBlockedList(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	ch := telemetryRepo.New(ti.chConn)
	now := time.Now().UTC()
	require.NoError(t, ch.UpsertShadowMCPInventoryURLs(ctx, []telemetryRepo.UpsertShadowMCPInventoryURLParams{
		{
			GramProjectID:      projectID,
			CanonicalServerURL: "https://sketchy.example.com/mcp",
			URLHost:            "sketchy.example.com",
			ServerName:         "Sketchy",
			SeenAt:             now.Add(-2 * time.Hour),
			FirstSeen:          now.Add(-2 * time.Hour),
			LastSeen:           now.Add(-2 * time.Hour),
			UpdatedAt:          now.Add(-2 * time.Hour),
		},
		{
			GramProjectID:      projectID,
			CanonicalServerURL: "https://fine.example.com/mcp",
			URLHost:            "fine.example.com",
			ServerName:         "Fine",
			SeenAt:             now.Add(-1 * time.Hour),
			FirstSeen:          now.Add(-1 * time.Hour),
			LastSeen:           now.Add(-1 * time.Hour),
			UpdatedAt:          now.Add(-1 * time.Hour),
		},
	}))

	_ = createShadowMCPInventoryPolicy(t, ctx, ti, shadowMCPInventoryPolicyInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
		Name:           "Allow All Shadow MCP",
		Action:         "block",
		Disposition:    "allow_all",
		BlockedURLs:    []string{"https://sketchy.example.com/mcp"},
	})

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	result, err := ti.service.ListShadowMCPInventory(ctx, &gen.ListShadowMCPInventoryPayload{
		ProjectID: projectID,
		Limit:     10,
	})
	require.NoError(t, err)
	require.Len(t, result.Servers, 2)

	sketchy := shadowMCPInventoryServerByURL(result.Servers, "https://sketchy.example.com/mcp")
	require.NotNil(t, sketchy)
	require.Equal(t, shadowMCPInventoryAccessBlocked, sketchy.Access)
	require.Empty(t, sketchy.AllowedPolicyIds)

	fine := shadowMCPInventoryServerByURL(result.Servers, "https://fine.example.com/mcp")
	require.NotNil(t, fine)
	// Under allow_all the default state reads as allowed, without any per-URL
	// grant backing it.
	require.Equal(t, shadowMCPInventoryAccessAllowed, fine.Access)
	require.Empty(t, fine.AllowedPolicyIds)
	require.Empty(t, fine.BlockedPolicyIds)
}

func TestService_ListShadowMCPInventory_TargetedBlockAllPolicyIsRestricted(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	ch := telemetryRepo.New(ti.chConn)
	now := time.Now().UTC()
	require.NoError(t, ch.UpsertShadowMCPInventoryURLs(ctx, []telemetryRepo.UpsertShadowMCPInventoryURLParams{
		{
			GramProjectID:      projectID,
			CanonicalServerURL: "https://scoped.example.com/mcp",
			URLHost:            "scoped.example.com",
			ServerName:         "Scoped",
			SeenAt:             now.Add(-1 * time.Hour),
			FirstSeen:          now.Add(-1 * time.Hour),
			LastSeen:           now.Add(-1 * time.Hour),
			UpdatedAt:          now.Add(-1 * time.Hour),
		},
	}))

	// A deny-by-default policy scoped to a subset of users blocks the server
	// for them only, so it must read as restricted rather than blocked.
	createShadowMCPInventoryPolicy(t, ctx, ti, shadowMCPInventoryPolicyInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
		Name:           "Targeted Block All",
		Action:         "block",
		Disposition:    "block_all",
		AudienceType:   "targeted",
	})

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	result, err := ti.service.ListShadowMCPInventory(ctx, &gen.ListShadowMCPInventoryPayload{
		ProjectID: projectID,
		Limit:     10,
	})
	require.NoError(t, err)
	require.Len(t, result.Servers, 1)

	scoped := shadowMCPInventoryServerByURL(result.Servers, "https://scoped.example.com/mcp")
	require.NotNil(t, scoped)
	require.Equal(t, shadowMCPInventoryAccessRestricted, scoped.Access)
}

func TestService_ListShadowMCPInventory_ScopedApprovalIsRestricted(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	ch := telemetryRepo.New(ti.chConn)
	now := time.Now().UTC()
	require.NoError(t, ch.UpsertShadowMCPInventoryURLs(ctx, []telemetryRepo.UpsertShadowMCPInventoryURLParams{
		{
			GramProjectID:      projectID,
			CanonicalServerURL: "https://scoped-allow.example.com/mcp",
			URLHost:            "scoped-allow.example.com",
			ServerName:         "Scoped Allow",
			SeenAt:             now.Add(-1 * time.Hour),
			FirstSeen:          now.Add(-1 * time.Hour),
			LastSeen:           now.Add(-1 * time.Hour),
			UpdatedAt:          now.Add(-1 * time.Hour),
		},
		{
			GramProjectID:      projectID,
			CanonicalServerURL: "https://everyone-allow.example.com/mcp",
			URLHost:            "everyone-allow.example.com",
			ServerName:         "Everyone Allow",
			SeenAt:             now.Add(-1 * time.Hour),
			FirstSeen:          now.Add(-1 * time.Hour),
			LastSeen:           now.Add(-1 * time.Hour),
			UpdatedAt:          now.Add(-1 * time.Hour),
		},
	}))

	policy := createShadowMCPInventoryPolicy(t, ctx, ti, shadowMCPInventoryPolicyInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
		Name:           "Block All Shadow MCP",
		Action:         "block",
		Disposition:    "block_all",
		AudienceType:   "everyone",
	})

	// A bypass grant naming one user lets that user through while the policy
	// still blocks everyone else — restricted, not allowed. The all-users
	// grant on the second URL is the everyone-approval and stays allowed.
	grantShadowMCPInventoryBypassForPrincipals(t, ctx, ti, authCtx.ActiveOrganizationID, policy.ID.String(), "https://scoped-allow.example.com/mcp", urn.NewPrincipal(urn.PrincipalTypeUser, "user_scoped_approval"))
	grantShadowMCPInventoryBypass(t, ctx, ti, authCtx.ActiveOrganizationID, policy.ID.String(), "https://everyone-allow.example.com/mcp")

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	result, err := ti.service.ListShadowMCPInventory(ctx, &gen.ListShadowMCPInventoryPayload{
		ProjectID: projectID,
		Limit:     10,
	})
	require.NoError(t, err)
	require.Len(t, result.Servers, 2)

	scoped := shadowMCPInventoryServerByURL(result.Servers, "https://scoped-allow.example.com/mcp")
	require.NotNil(t, scoped)
	require.Equal(t, shadowMCPInventoryAccessRestricted, scoped.Access)
	require.NotNil(t, scoped.AccessSummary)
	require.Equal(t, shadowMCPAccessStateRestricted, scoped.AccessSummary.State)
	require.Equal(t, shadowMCPAccessReachSelected, scoped.AccessSummary.AllowedFor)
	require.Equal(t, shadowMCPAccessReachNone, scoped.AccessSummary.BlockedFor)
	require.Equal(t, shadowMCPAccessDefaultDeny, scoped.AccessSummary.BlockingDefault)
	require.Nil(t, scoped.AccessSummary.Decision)
	require.Equal(t, shadowMCPAccessCoverageNone, scoped.AccessSummary.DecisionCoverage)

	everyone := shadowMCPInventoryServerByURL(result.Servers, "https://everyone-allow.example.com/mcp")
	require.NotNil(t, everyone)
	require.Equal(t, shadowMCPInventoryAccessAllowed, everyone.Access)
	require.NotNil(t, everyone.AccessSummary)
	require.Equal(t, shadowMCPAccessStateAllowed, everyone.AccessSummary.State)
	require.Equal(t, shadowMCPAccessReachEveryone, everyone.AccessSummary.AllowedFor)
}

func TestService_ListShadowMCPInventory_TargetedPolicyFullAudienceGrantIsAllowed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	ch := telemetryRepo.New(ti.chConn)
	now := time.Now().UTC()
	require.NoError(t, ch.UpsertShadowMCPInventoryURLs(ctx, []telemetryRepo.UpsertShadowMCPInventoryURLParams{
		{
			GramProjectID:      projectID,
			CanonicalServerURL: "https://audience-covered.example.com/mcp",
			URLHost:            "audience-covered.example.com",
			ServerName:         "Audience Covered",
			SeenAt:             now.Add(-1 * time.Hour),
			FirstSeen:          now.Add(-1 * time.Hour),
			LastSeen:           now.Add(-1 * time.Hour),
			UpdatedAt:          now.Add(-1 * time.Hour),
		},
	}))

	// A deny-by-default policy targeted at one role blocks only that role. A
	// bypass grant naming the same role frees everyone the policy ever
	// blocked — users outside the audience were never blocked — so the
	// server is effectively open to the whole project and must read allowed,
	// not restricted. Comparing grants against the literal all-users
	// principal alone got this wrong.
	policy := createShadowMCPInventoryPolicy(t, ctx, ti, shadowMCPInventoryPolicyInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
		Name:           "Targeted Block All Covered",
		Action:         "block",
		Disposition:    "block_all",
		AudienceType:   "targeted",
	})
	engineering := urn.NewPrincipal(urn.PrincipalTypeRole, "engineering")
	grantShadowMCPInventoryPolicyAudience(t, ctx, ti, authCtx.ActiveOrganizationID, policy.ID.String(), engineering)
	grantShadowMCPInventoryBypassForPrincipals(t, ctx, ti, authCtx.ActiveOrganizationID, policy.ID.String(), "https://audience-covered.example.com/mcp", engineering)

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	result, err := ti.service.ListShadowMCPInventory(ctx, &gen.ListShadowMCPInventoryPayload{
		ProjectID: projectID,
		Limit:     10,
	})
	require.NoError(t, err)
	require.Len(t, result.Servers, 1)

	covered := shadowMCPInventoryServerByURL(result.Servers, "https://audience-covered.example.com/mcp")
	require.NotNil(t, covered)
	require.Equal(t, shadowMCPInventoryAccessAllowed, covered.Access)
	require.NotNil(t, covered.AccessSummary)
	require.Equal(t, shadowMCPAccessStateAllowed, covered.AccessSummary.State)
	require.Equal(t, shadowMCPAccessReachEveryone, covered.AccessSummary.AllowedFor)
}

func TestService_ListShadowMCPInventory_StdioOnlyPageLoadsPolicyPosture(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	// No observed URLs at all: the page's only row is a denied stdio review.
	// The policy posture must still load — one row's verdict cannot depend on
	// which other rows share the page — and the denial must read as recorded
	// but uncarried, since stdio decisions write no enforcement.
	createShadowMCPInventoryPolicy(t, ctx, ti, shadowMCPInventoryPolicyInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
		Name:           "Block All For Stdio Page",
		Action:         "block",
		Disposition:    "block_all",
		AudienceType:   "everyone",
	})

	queries := mcpapprovalrepo.New(ti.conn)
	_, err := queries.UpsertApprovalRequest(ctx, mcpapprovalrepo.UpsertApprovalRequestParams{
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		TargetKind:                "stdio_command",
		TargetRaw:                 "npx -y stdio-only-package",
		TargetKey:                 "npx -y stdio-only-package",
		ArtifactRef:               conv.ToPGTextEmpty(""),
		VersionPinned:             false,
		Status:                    "denied",
		RiskPolicyBypassRequestID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)

	result, err := ti.service.ListShadowMCPInventory(ctx, &gen.ListShadowMCPInventoryPayload{
		ProjectID: projectID,
		Limit:     10,
	})
	require.NoError(t, err)
	require.Len(t, result.Servers, 1)

	row := result.Servers[0]
	require.Equal(t, "npx -y stdio-only-package", row.CanonicalServerURL)
	require.NotNil(t, row.AccessSummary)
	// Deny-by-default posture reaches local commands too.
	require.Equal(t, shadowMCPAccessStateBlocked, row.AccessSummary.State)
	require.Equal(t, shadowMCPAccessDefaultDeny, row.AccessSummary.BlockingDefault)
	require.NotNil(t, row.AccessSummary.Decision)
	require.Equal(t, "denied", *row.AccessSummary.Decision)
	// Recorded, not enforced: no grant writer acts on stdio targets.
	require.Equal(t, shadowMCPAccessCoverageNone, row.AccessSummary.DecisionCoverage)
	// Legacy parity: the deprecated field keeps its historical stdio value.
	require.Equal(t, shadowMCPInventoryAccessNone, row.Access)
}

func TestService_ResolveShadowMCPInventoryRequest_AllowAllApprovalUnblocksURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	blockedURL := "https://mcp.example.com/mcp"
	policy := createShadowMCPInventoryPolicy(t, ctx, ti, shadowMCPInventoryPolicyInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
		Name:           "Allow All Shadow MCP",
		Action:         "block",
		Disposition:    "allow_all",
		BlockedURLs:    []string{blockedURL, "https://other.example.com/mcp"},
	})
	requestID := createShadowMCPInventoryBypassRequest(t, ctx, ti, shadowMCPInventoryBypassRequestInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
		PolicyID:       policy.ID.String(),
		ServerURL:      blockedURL,
		RequesterID:    "user_one",
		RequesterEmail: "one@example.com",
		RequestedAt:    time.Now(),
	})

	// No policy ids in the payload: approval under allow_all is a
	// project-wide unblock, not a policy selection.
	result, err := ti.service.ResolveShadowMCPInventoryRequest(ctx, &gen.ResolveShadowMCPInventoryRequestPayload{
		ProjectID: projectID,
		ServerURL: blockedURL,
		Decision:  "allow",
	})
	require.NoError(t, err)
	require.Equal(t, shadowMCPInventoryAccessAllowed, result.Access)
	require.Equal(t, 0, result.RequestCount)
	require.Empty(t, result.AllowedPolicyIds)

	require.Equal(t, "approved", shadowMCPInventoryBypassRequestStatus(t, ctx, ti, projectID, requestID))
	require.Empty(t, shadowMCPInventoryBypassGrantPrincipals(t, ctx, ti, authCtx.ActiveOrganizationID, policy.ID.String(), blockedURL))

	// Only the approved URL's block grant is revoked; the other stays.
	blockGrants, err := authz.ListGrantsForResource(ctx, ti.conn, authz.Resource{
		OrganizationID: authCtx.ActiveOrganizationID,
		Scope:          authz.ScopeRiskPolicyBlock,
		ResourceID:     policy.ID.String(),
	})
	require.NoError(t, err)
	require.Len(t, blockGrants, 1)
	require.Equal(t, "https://other.example.com/mcp", blockGrants[0].Selector[authz.SelectorKeyServerURL])
}

func seedShadowMCPApprovalRequest(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, projectID uuid.UUID, canonicalURL string, status string, requesterCount int) mcpapprovalrepo.UpsertApprovalRequestRow {
	t.Helper()

	queries := mcpapprovalrepo.New(ti.conn)
	request, err := queries.UpsertApprovalRequest(ctx, mcpapprovalrepo.UpsertApprovalRequestParams{
		OrganizationID:            organizationID,
		ProjectID:                 projectID,
		TargetKind:                "server_url",
		TargetRaw:                 canonicalURL,
		TargetKey:                 canonicalURL,
		ArtifactRef:               conv.ToPGTextEmpty(""),
		VersionPinned:             false,
		Status:                    status,
		RiskPolicyBypassRequestID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)

	for range requesterCount {
		_, err := queries.UpsertApprovalRequestRequester(ctx, mcpapprovalrepo.UpsertApprovalRequestRequesterParams{
			OrganizationID:       organizationID,
			ProjectID:            projectID,
			McpApprovalRequestID: request.ID,
			UserID:               uuid.NewString(),
			UserEmail:            conv.ToPGText("requester-" + uuid.NewString()[:8] + "@example.com"),
			Note:                 conv.ToPGTextEmpty(""),
		})
		require.NoError(t, err)
	}

	return request
}

func TestService_ShadowMCPInventory_JoinsApprovalRequestState(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	now := time.Now().UTC()

	require.NoError(t, telemetryRepo.New(ti.chConn).UpsertShadowMCPInventoryURLs(ctx, []telemetryRepo.UpsertShadowMCPInventoryURLParams{
		{
			GramProjectID:      projectID,
			CanonicalServerURL: "https://tracked.example.com/mcp",
			URLHost:            "tracked.example.com",
			ServerName:         "Tracked MCP",
			SeenAt:             now.Add(-2 * time.Hour),
			FirstSeen:          now.Add(-2 * time.Hour),
			LastSeen:           now.Add(-1 * time.Hour),
			UpdatedAt:          now.Add(-1 * time.Hour),
		},
		{
			GramProjectID:      projectID,
			CanonicalServerURL: "https://untracked.example.com/mcp",
			URLHost:            "untracked.example.com",
			ServerName:         "Untracked MCP",
			SeenAt:             now.Add(-2 * time.Hour),
			FirstSeen:          now.Add(-2 * time.Hour),
			LastSeen:           now.Add(-1 * time.Hour),
			UpdatedAt:          now.Add(-1 * time.Hour),
		},
	}))

	request := seedShadowMCPApprovalRequest(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "https://tracked.example.com/mcp", "requested", 2)

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	result, err := ti.service.ListShadowMCPInventory(ctx, &gen.ListShadowMCPInventoryPayload{
		ProjectID: projectID,
		Limit:     10,
	})
	require.NoError(t, err)
	require.Len(t, result.Servers, 2)

	byURL := make(map[string]*gen.ShadowMCPInventoryServer, len(result.Servers))
	for _, server := range result.Servers {
		byURL[server.CanonicalServerURL] = server
	}

	tracked := byURL["https://tracked.example.com/mcp"]
	require.NotNil(t, tracked.ApprovalRequest)
	require.Equal(t, request.ID.String(), tracked.ApprovalRequest.ID)
	require.Equal(t, "requested", tracked.ApprovalRequest.Status)
	require.Equal(t, 2, tracked.ApprovalRequest.RequesterCount)

	require.Nil(t, byURL["https://untracked.example.com/mcp"].ApprovalRequest)

	detail, err := ti.service.GetShadowMCPInventoryServer(ctx, &gen.GetShadowMCPInventoryServerPayload{
		ProjectID:  projectID,
		ServerSlug: tracked.ServerSlug,
	})
	require.NoError(t, err)
	require.NotNil(t, detail.ApprovalRequest)
	require.Equal(t, request.ID.String(), detail.ApprovalRequest.ID)
	require.Equal(t, 2, detail.ApprovalRequest.RequesterCount)
}

func TestService_GetShadowMCPInventoryServer_ResolvesRequestOnlyServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	request := seedShadowMCPApprovalRequest(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "https://requested-only.example.com/mcp", "denied", 1)

	server, err := ti.service.GetShadowMCPInventoryServer(ctx, &gen.GetShadowMCPInventoryServerPayload{
		ProjectID:  authCtx.ProjectID.String(),
		ServerSlug: shadowmcp.ServerSlug("https://requested-only.example.com/mcp"),
	})
	require.NoError(t, err)

	require.Equal(t, "https://requested-only.example.com/mcp", server.CanonicalServerURL)
	require.Equal(t, "requested-only.example.com", server.URLHost)
	require.Equal(t, 0, server.ObservedUseCount)
	require.Equal(t, 0, server.UserCount)
	require.Empty(t, server.TopUsers)
	require.Nil(t, server.LastCalled)
	require.NotNil(t, server.ApprovalRequest)
	require.Equal(t, request.ID.String(), server.ApprovalRequest.ID)
	require.Equal(t, "denied", server.ApprovalRequest.Status)
	require.Equal(t, 1, server.ApprovalRequest.RequesterCount)

	_, err = ti.service.GetShadowMCPInventoryServer(ctx, &gen.GetShadowMCPInventoryServerPayload{
		ProjectID:  authCtx.ProjectID.String(),
		ServerSlug: shadowmcp.ServerSlug("https://never-requested.example.com/mcp"),
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func seedShadowMCPStdioApprovalRequest(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, projectID uuid.UUID, command string, status string) mcpapprovalrepo.UpsertApprovalRequestRow {
	t.Helper()

	request, err := mcpapprovalrepo.New(ti.conn).UpsertApprovalRequest(ctx, mcpapprovalrepo.UpsertApprovalRequestParams{
		OrganizationID:            organizationID,
		ProjectID:                 projectID,
		TargetKind:                "stdio_command",
		TargetRaw:                 command,
		TargetKey:                 command,
		ArtifactRef:               conv.ToPGTextEmpty(""),
		VersionPinned:             false,
		Status:                    status,
		RiskPolicyBypassRequestID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)

	return request
}

func TestService_ListShadowMCPInventory_UnionsRequestOnlyTargets(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	now := time.Now().UTC()

	require.NoError(t, telemetryRepo.New(ti.chConn).UpsertShadowMCPInventoryURLs(ctx, []telemetryRepo.UpsertShadowMCPInventoryURLParams{
		{
			GramProjectID:      projectID,
			CanonicalServerURL: "https://observed.example.com/mcp",
			URLHost:            "observed.example.com",
			ServerName:         "Observed MCP",
			SeenAt:             now.Add(-2 * time.Hour),
			FirstSeen:          now.Add(-2 * time.Hour),
			LastSeen:           now.Add(-1 * time.Hour),
			UpdatedAt:          now.Add(-1 * time.Hour),
		},
		// A second observed server so a one-row page spills past the limit
		// and produces a real cursor for the later-pages assertion.
		{
			GramProjectID:      projectID,
			CanonicalServerURL: "https://observed-2.example.com/mcp",
			URLHost:            "observed-2.example.com",
			ServerName:         "Observed MCP Two",
			SeenAt:             now.Add(-4 * time.Hour),
			FirstSeen:          now.Add(-4 * time.Hour),
			LastSeen:           now.Add(-3 * time.Hour),
			UpdatedAt:          now.Add(-3 * time.Hour),
		},
	}))

	// Observed AND requested: must appear once, as the observed row.
	seedShadowMCPApprovalRequest(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "https://observed.example.com/mcp", "requested", 2)
	// Requested, never observed: appears as a synthesized first-page row.
	urlOnly := seedShadowMCPApprovalRequest(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "https://asked-only.example.com/mcp", "denied", 1)
	// Stdio commands are known only through their reviews.
	stdio := seedShadowMCPStdioApprovalRequest(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "npx -y example-package", "requested")

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	result, err := ti.service.ListShadowMCPInventory(ctx, &gen.ListShadowMCPInventoryPayload{
		ProjectID: projectID,
		Limit:     10,
	})
	require.NoError(t, err)
	require.Len(t, result.Servers, 4)

	byURL := make(map[string]*gen.ShadowMCPInventoryServer, len(result.Servers))
	for _, server := range result.Servers {
		byURL[server.CanonicalServerURL] = server
	}

	observed := byURL["https://observed.example.com/mcp"]
	require.Equal(t, "server_url", *observed.TargetKind)
	require.NotNil(t, observed.ApprovalRequest)
	require.Equal(t, "requested", observed.ApprovalRequest.Status)

	askedOnly := byURL["https://asked-only.example.com/mcp"]
	require.Equal(t, "server_url", *askedOnly.TargetKind)
	require.Equal(t, 0, askedOnly.ObservedUseCount)
	require.NotNil(t, askedOnly.ApprovalRequest)
	require.Equal(t, urlOnly.ID.String(), askedOnly.ApprovalRequest.ID)
	require.Equal(t, "denied", askedOnly.ApprovalRequest.Status)

	stdioRow := byURL["npx -y example-package"]
	require.NotNil(t, stdioRow)
	require.Equal(t, "stdio_command", *stdioRow.TargetKind)
	require.Empty(t, stdioRow.URLHost)
	require.NotNil(t, stdioRow.ApprovalRequest)
	require.Equal(t, stdio.ID.String(), stdioRow.ApprovalRequest.ID)

	// Later pages carry no synthesized rows: a one-row page spills the
	// observed set past the limit, and the cursor-driven second page holds
	// only observed servers.
	firstPage, err := ti.service.ListShadowMCPInventory(ctx, &gen.ListShadowMCPInventoryPayload{
		ProjectID: projectID,
		Limit:     1,
	})
	require.NoError(t, err)
	require.NotNil(t, firstPage.NextCursor)

	paged, err := ti.service.ListShadowMCPInventory(ctx, &gen.ListShadowMCPInventoryPayload{
		ProjectID: projectID,
		Limit:     1,
		Cursor:    firstPage.NextCursor,
	})
	require.NoError(t, err)
	require.NotEmpty(t, paged.Servers)
	for _, server := range paged.Servers {
		require.Equal(t, "server_url", *server.TargetKind)
		require.NotEqual(t, "npx -y example-package", server.CanonicalServerURL)
		require.NotEqual(t, "https://asked-only.example.com/mcp", server.CanonicalServerURL)
	}
}
