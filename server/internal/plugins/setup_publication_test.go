package plugins_test

import (
	"testing"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	publicationv1 "github.com/speakeasy-api/gram/infra/gen/gram/plugins/v1"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/roledistribution"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

// The broker is not exercised: setup enqueues the real publication event, then
// the public publication service renders and pushes through the mock publisher.
func TestRoleSetupPublishesExistingGrantedServer(t *testing.T) {
	t.Parallel()
	publisher := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, publisher)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	role := createTestRolePrincipal(t, ctx, ti, "setup-backfill")
	server := createTestToolset(t, ctx, ti.conn, "Backfilled server")
	principal, err := urn.ParsePrincipal(role)
	require.NoError(t, err)
	selectors, err := authz.NewSelector(authz.ScopeMCPConnect, server.ID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: principal, Scope: string(authz.ScopeMCPConnect), Selectors: selectors})
	require.NoError(t, err)
	_, err = ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "setup-backfill"})
	require.NoError(t, err)
	// Establish the existing marketplace required by transactional refresh hints.
	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	const artifact = "cursor-plugins/setup-backfill-cursor/mcp.json"
	require.NotContains(t, string(publisher.lastPushedFiles[artifact]), server.McpSlug.String)
	fixtures := testrepo.New(ti.conn)
	enabled, err := fixtures.SourceRoleDistributionSetupEnabled(ctx, ac.ActiveOrganizationID)
	require.NoError(t, err)
	if !enabled {
		require.NoError(t, fixtures.PipelineEnableRoleDistribution(ctx, ac.ActiveOrganizationID))
	}
	before, err := fixtures.ListPublishOutboxRows(ctx)
	require.NoError(t, err)
	beforeIDs := make(map[int64]bool, len(before))
	for _, row := range before {
		beforeIDs[row.ID] = true
	}
	processed, err := roledistribution.ProcessRoleDistributionSetup(ctx, ti.conn, plugins.PublicationRequests{Enabled: true}, nil, role, ac.ActiveOrganizationID)
	require.NoError(t, err)
	require.True(t, processed)
	events, err := fixtures.ListPublishOutboxRows(ctx)
	require.NoError(t, err)
	var requested *publicationv1.PublicationRequested
	for _, row := range events {
		if beforeIDs[row.ID] || row.Topic != "gram.plugins.v1.PublicationRequested" {
			continue
		}
		event := &publicationv1.PublicationRequested{}
		require.NoError(t, proto.Unmarshal(row.Message, event))
		if event.GetOrganizationId() == ac.ActiveOrganizationID && event.GetProjectId() == ac.ProjectID.String() {
			requested = event
		}
	}
	require.NotNil(t, requested, "setup requests a refresh for the affected organization and project")
	require.Equal(t, ac.ActiveOrganizationID, requested.GetOrganizationId())
	require.Equal(t, ac.ProjectID.String(), requested.GetProjectId())
	_, err = ti.service.PublishProject(ctx, plugins.PublishProjectInput{
		ProjectID:       uuid.MustParse(requested.GetProjectId()),
		CreatedByUserID: requested.GetCreatedByUserId(),
		CommitMessage:   "Refresh role setup",
		SkipIfUnchanged: true,
	})
	require.NoError(t, err)
	content, exists := publisher.lastPushedFiles[artifact]
	require.True(t, exists, "setup role plugin must produce a client package")
	require.Contains(t, string(content), server.McpSlug.String, "published package includes the existing granted server without manual content edits")
}
