package plugins_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	endpointrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcprepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins/roledelivery"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestPlatformCleanupPreservesManualAndPublishes(t *testing.T) {
	t.Parallel()
	for _, wrapped := range []bool{false, true} {
		name := "direct"
		if wrapped {
			name = "wrapped"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			publisher := &mockGitHubPublisher{}
			ctx, ti := newTestPluginsServiceWithGitHub(t, publisher)
			ac, _ := contextvalues.GetAuthContext(ctx)
			projectID, org := *ac.ProjectID, ac.ActiveOrganizationID
			backend := createTestToolset(t, ctx, ti.conn, "cleanup-"+name)
			toolsetID := backend.ID.String()
			manualPayload := &gen.AddPluginServerPayload{ToolsetID: &toolsetID, Policy: "required"}
			endpointSlug := backend.McpSlug.String
			if wrapped {
				wrapperID := uuid.New()
				_, err := mcprepo.New(ti.conn).CreateMCPServer(ctx, mcprepo.CreateMCPServerParams{ID: wrapperID, ProjectID: projectID, Name: pgtype.Text{String: "Cleanup wrapper", Valid: true}, Slug: pgtype.Text{String: "cleanup-wrapper", Valid: true}, ToolsetID: uuid.NullUUID{UUID: backend.ID, Valid: true}, Visibility: "private"})
				require.NoError(t, err)
				endpointSlug = "cleanup-wrapper"
				_, err = endpointrepo.New(ti.conn).CreateMCPEndpoint(ctx, endpointrepo.CreateMCPEndpointParams{ProjectID: projectID, McpServerID: uuid.NullUUID{UUID: wrapperID, Valid: true}, Slug: endpointSlug})
				require.NoError(t, err)
				id := wrapperID.String()
				manualPayload.ToolsetID, manualPayload.McpServerID = nil, &id
			}
			role := createTestRolePrincipal(t, ctx, ti, "cleanup")
			principal, err := urn.ParsePrincipal(role)
			require.NoError(t, err)
			selectors, err := authz.NewSelector(authz.ScopeMCPConnect, "*").MarshalJSON()
			require.NoError(t, err)
			_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: org, PrincipalUrn: principal, Scope: string(authz.ScopeMCPConnect), Selectors: selectors})
			require.NoError(t, err)
			automatic, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Cleanup automatic"})
			require.NoError(t, err)
			manual, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Cleanup manual"})
			require.NoError(t, err)
			manualPayload.PluginID = manual.ID
			manualMembership, err := ti.service.AddPluginServer(ctx, manualPayload)
			require.NoError(t, err)
			_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: automatic.ID, PrincipalUrns: []string{role}})
			require.NoError(t, err)
			before, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: automatic.ID})
			require.NoError(t, err)
			require.Len(t, before.Servers, 1)
			membershipID := uuid.MustParse(before.Servers[0].ID)
			platform, err := urn.ParseTool("tools:platform:slack:send_message")
			require.NoError(t, err)
			_, err = toolsetsrepo.New(ti.conn).CreateToolsetVersion(ctx, toolsetsrepo.CreateToolsetVersionParams{ToolsetID: backend.ID, Version: 1, ToolUrns: []urn.Tool{platform}, ResourceUrns: []urn.Resource{}})
			require.NoError(t, err)

			// Stale previews cannot delete an entry whose current content is ordinary.
			tx := testenv.BeginTx(t, ctx, ti.conn)
			require.NoError(t, admission.LockProject(ctx, tx, projectID))
			_, err = toolsetsrepo.New(tx).CreateToolsetVersion(ctx, toolsetsrepo.CreateToolsetVersionParams{ToolsetID: backend.ID, Version: 2, ToolUrns: []urn.Tool{}, ResourceUrns: []urn.Resource{}})
			require.NoError(t, err)
			removed, err := roledelivery.ApplyPlatformCleanup(ctx, tx, org, projectID, []uuid.UUID{membershipID})
			require.NoError(t, err)
			require.Empty(t, removed)
			require.NoError(t, tx.Rollback(ctx))

			// The caller owns publication configuration; audit, removals and publication
			// commit together, exactly as the toolset mutation services do.
			_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
			require.NoError(t, err)
			require.Contains(t, string(publisher.lastPushedFiles["cursor-plugins/cleanup-automatic-cursor/mcp.json"]), endpointSlug)
			beforeEvents, err := testrepo.New(ti.conn).ListPublishOutboxRows(ctx)
			require.NoError(t, err)
			tx = testenv.BeginTx(t, ctx, ti.conn)
			removed, err = roledelivery.ContentChanged(ctx, tx, org, projectID, backend.ID, nil)
			require.NoError(t, err)
			require.NoError(t, (plugins.PublicationRequests{Enabled: true}).Project(ctx, tx, org, projectID, ac.UserID))
			require.NoError(t, err)
			require.Equal(t, []uuid.UUID{uuid.MustParse(automatic.ID)}, removed)
			pendingEvents, err := testrepo.New(tx).ListPublishOutboxRows(ctx)
			require.NoError(t, err)
			require.Greater(t, len(pendingEvents), len(beforeEvents))
			require.NoError(t, tx.Rollback(ctx))
			rolledBackEvents, err := testrepo.New(ti.conn).ListPublishOutboxRows(ctx)
			require.NoError(t, err)
			require.Len(t, rolledBackEvents, len(beforeEvents))
			restored, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: automatic.ID})
			require.NoError(t, err)
			require.Len(t, restored.Servers, 1)
			tx = testenv.BeginTx(t, ctx, ti.conn)
			require.NoError(t, admission.LockProject(ctx, tx, projectID))
			removed, err = roledelivery.ContentChanged(ctx, tx, org, projectID, backend.ID, nil)
			require.NoError(t, err)
			require.Equal(t, []uuid.UUID{uuid.MustParse(automatic.ID)}, removed)
			require.NoError(t, (plugins.PublicationRequests{Enabled: true}).Project(ctx, tx, org, projectID, ac.UserID))
			replay, err := roledelivery.ApplyPlatformCleanup(ctx, tx, org, projectID, []uuid.UUID{membershipID, uuid.MustParse(manualMembership.ID)})
			require.NoError(t, err)
			require.Empty(t, replay)
			require.NoError(t, tx.Commit(ctx))
			got, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: automatic.ID})
			require.NoError(t, err)
			require.Empty(t, got.Servers)
			got, err = ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: manual.ID})
			require.NoError(t, err)
			require.Len(t, got.Servers, 1)
			events, err := testrepo.New(ti.conn).ListPublishOutboxRows(ctx)
			require.NoError(t, err)
			found := false
			for _, event := range events {
				if event.Topic == "gram.plugins.v1.PublicationRequested" {
					found = true
				}
			}
			require.True(t, found)
			_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
			require.NoError(t, err)
			require.NotContains(t, string(publisher.lastPushedFiles["cursor-plugins/cleanup-automatic-cursor/mcp.json"]), endpointSlug)
			require.Contains(t, string(publisher.lastPushedFiles["cursor-plugins/cleanup-manual-cursor/mcp.json"]), endpointSlug)

			// Removing the last platform tool does not undo existing deletion history.
			tx = testenv.BeginTx(t, ctx, ti.conn)
			require.NoError(t, admission.LockProject(ctx, tx, projectID))
			_, err = toolsetsrepo.New(tx).CreateToolsetVersion(ctx, toolsetsrepo.CreateToolsetVersionParams{ToolsetID: backend.ID, Version: 2, ToolUrns: []urn.Tool{}, ResourceUrns: []urn.Resource{}})
			require.NoError(t, err)
			removed, err = roledelivery.ContentChanged(ctx, tx, org, projectID, backend.ID, nil)
			require.NoError(t, err)
			require.Empty(t, removed)
			memberships, err := pluginsrepo.New(tx).ListPluginServers(ctx, uuid.MustParse(automatic.ID))
			require.NoError(t, err)
			require.Empty(t, memberships)
			require.NoError(t, tx.Rollback(ctx))
		})
	}
}
