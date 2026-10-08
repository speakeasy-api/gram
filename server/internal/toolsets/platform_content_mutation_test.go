package toolsets_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins/roledelivery"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

// Arrange membership through the real audience event so audit provenance
// identifies automatic delivery.
func seedContentMutationMemberships(t *testing.T, ctx context.Context, ti *testInstance, toolsetID uuid.UUID) (uuid.UUID, uuid.UUID, string) {
	t.Helper()
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	q := pluginsrepo.New(ti.conn)
	automatic, err := q.CreatePlugin(ctx, pluginsrepo.CreatePluginParams{OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, Name: "Automatic", Slug: "automatic"})
	require.NoError(t, err)
	manual, err := q.CreatePlugin(ctx, pluginsrepo.CreatePluginParams{OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, Name: "Manual", Slug: "manual"})
	require.NoError(t, err)
	role, err := accessrepo.New(ti.conn).UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{OrganizationID: ac.ActiveOrganizationID, WorkosSlug: "content-edit", WorkosName: "Content edit", WorkosCreatedAt: conv.ToPGTimestamptz(time.Now()), WorkosUpdatedAt: conv.ToPGTimestamptz(time.Now())})
	require.NoError(t, err)
	tx := testenv.BeginTx(t, ctx, ti.conn)
	_, err = authz.PatchRoleGrantsTx(ctx, tx, ac.ActiveOrganizationID, "content-edit", role.RoleUrn, []*authz.RoleGrant{{Scope: string(authz.ScopeMCPConnect), Selectors: []authz.Selector{authz.NewGrant(authz.ScopeMCPConnect, toolsetID.String()).Selector}}}, nil)
	require.NoError(t, err)
	_, err = pluginsrepo.New(tx).AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: role.RoleUrn, PluginID: automatic.ID})
	require.NoError(t, err)
	changed, err := roledelivery.AudienceChanged(ctx, tx, ac.ActiveOrganizationID, *ac.ProjectID, automatic.ID, nil, []string{role.RoleUrn}, nil)
	require.NoError(t, err)
	require.True(t, changed)
	require.NoError(t, tx.Commit(ctx))
	_, err = q.AddPluginServer(ctx, pluginsrepo.AddPluginServerParams{PluginID: manual.ID, ToolsetID: uuid.NullUUID{UUID: toolsetID, Valid: true}, DisplayName: "Manual server", Policy: "optional"})
	require.NoError(t, err)
	return automatic.ID, manual.ID, role.RoleUrn
}

func TestUpdateToolsetPlatformContentRemovesOnlyAutomaticDelivery(t *testing.T) {
	t.Parallel()
	ctx, ti, carried := newPublishingToolsetsService(t, true)
	id := uuid.MustParse(carried.ID)
	dep := createPetstoreDeployment(t, ctx, ti)
	tools, err := testrepo.New(ti.conn).ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err)
	require.NotEmpty(t, tools)
	_, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: carried.Slug, ToolUrns: []string{tools[0].ToolUrn.String()}})
	require.NoError(t, err)
	automatic, manual, role := seedContentMutationMemberships(t, ctx, ti, id)
	before := publicationRequests(t, ctx, ti)
	beforeAudit, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginServerRemove)
	require.NoError(t, err)
	platform := urn.NewTool(urn.ToolKindPlatform, "logs", "search_logs")
	_, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: carried.Slug, ToolUrns: []string{tools[0].ToolUrn.String(), platform.String()}})
	require.NoError(t, err)
	q := pluginsrepo.New(ti.conn)
	autoRows, err := q.ListPluginServers(ctx, automatic)
	require.NoError(t, err)
	require.Empty(t, autoRows)
	manualRows, err := q.ListPluginServers(ctx, manual)
	require.NoError(t, err)
	require.Len(t, manualRows, 1)
	require.Greater(t, publicationRequests(t, ctx, ti), before)
	afterAudit, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginServerRemove)
	require.NoError(t, err)
	require.Greater(t, afterAudit, beforeAudit)
	ac, _ := contextvalues.GetAuthContext(ctx)
	principal, err := urn.ParsePrincipal(role)
	require.NoError(t, err)
	grants, err := authz.LoadGrants(ctx, ti.conn, ac.ActiveOrganizationID, []urn.Principal{principal})
	require.NoError(t, err)
	allowed, err := authz.GrantsAuthorize(grants, authz.MCPCheck(authz.ScopeMCPConnect, id.String(), ac.ProjectID.String()))
	require.NoError(t, err)
	require.True(t, allowed, "the same role must still be able to connect to the toolset")
}

func TestChangeToolsetToolsPlatformCleanupRollsBackWithContent(t *testing.T) {
	t.Parallel()
	ctx, ti, carried := newPublishingToolsetsService(t, true)
	id := uuid.MustParse(carried.ID)
	automatic, manual, _ := seedContentMutationMemberships(t, ctx, ti, id)
	ac, _ := contextvalues.GetAuthContext(ctx)
	before, err := toolsetsrepo.New(ti.conn).GetLatestToolsetVersion(ctx, id)
	require.NoError(t, err)
	count := publicationRequests(t, ctx, ti)
	tx := testenv.BeginTx(t, ctx, ti.conn)
	result, err := toolsets.ChangeToolsetToolsInTransaction(ctx, tx, testenv.NewLogger(t), audit.NewLogger(), ac, id, before.Version, toolsets.ToolExposureChange{Add: []urn.Tool{urn.NewTool(urn.ToolKindPlatform, "logs", "search_logs")}})
	require.NoError(t, err)
	require.Contains(t, result.RemovedPluginIDs, automatic)
	require.NotContains(t, result.RemovedPluginIDs, manual)
	// The caller queues publication in the content transaction, just as the
	// MCP mutation service does. Neither may escape a rollback.
	outcome, err := (plugins.PublicationRequests{Enabled: true}).ProjectWithOutcome(ctx, tx, ac.ActiveOrganizationID, *ac.ProjectID, ac.UserID)
	require.NoError(t, err)
	require.Equal(t, plugins.ProjectPublicationEnqueued, outcome)
	pending, err := testrepo.New(tx).CountPublishOutboxRowsByTopic(ctx, testrepo.CountPublishOutboxRowsByTopicParams{OrganizationID: ac.ActiveOrganizationID, Topic: publicationRequestedTopic})
	require.NoError(t, err)
	require.Equal(t, count+1, pending)
	rows, err := pluginsrepo.New(tx).ListPluginServers(ctx, automatic)
	require.NoError(t, err)
	require.Empty(t, rows)
	require.NoError(t, tx.Rollback(ctx))
	rows, err = pluginsrepo.New(ti.conn).ListPluginServers(ctx, automatic)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	after, err := toolsetsrepo.New(ti.conn).GetLatestToolsetVersion(ctx, id)
	require.NoError(t, err)
	require.Equal(t, before.Version, after.Version)
	require.Equal(t, count, publicationRequests(t, ctx, ti))
}

func TestUpdateToolsetRemovingLastPlatformToolRestoresEligibility(t *testing.T) {
	t.Parallel()
	ctx, ti, carried := newPublishingToolsetsService(t, true)
	id := uuid.MustParse(carried.ID)
	automatic, _, role := seedContentMutationMemberships(t, ctx, ti, id)
	platform := urn.NewTool(urn.ToolKindPlatform, "logs", "search_logs")
	_, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: carried.Slug, ToolUrns: []string{platform.String()}})
	require.NoError(t, err)
	ac, _ := contextvalues.GetAuthContext(ctx)
	q := pluginsrepo.New(ti.conn)
	fresh, err := q.CreatePlugin(ctx, pluginsrepo.CreatePluginParams{OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, Name: "New audience", Slug: "new-audience"})
	require.NoError(t, err)
	_, err = q.AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: role, PluginID: fresh.ID})
	require.NoError(t, err)
	before := publicationRequests(t, ctx, ti)
	_, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: carried.Slug, ToolUrns: []string{}})
	require.NoError(t, err)
	rows, err := q.ListPluginServers(ctx, fresh.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1, "removing the last platform tool restores delivery to new audiences")
	rows, err = q.ListPluginServers(ctx, automatic)
	require.NoError(t, err)
	require.Empty(t, rows, "eligibility replay preserves prior removal history")
	require.Greater(t, publicationRequests(t, ctx, ti), before)
}
