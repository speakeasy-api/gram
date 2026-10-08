package plugins_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/plugins/roledelivery"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

// These fixtures deliberately have no assistant association: tool contents alone
// must keep an orphaned or detached toolset out of automatic distribution.
func TestPlatformToolsetAutomaticDistribution(t *testing.T) {
	t.Parallel()
	platform := urn.NewTool(urn.ToolKindPlatform, "slack", "send_message")
	ordinary := urn.NewTool(urn.ToolKindHTTP, "example", "get_status")
	for _, wrapped := range []bool{false, true} {
		model := "direct"
		if wrapped {
			model = "wrapped"
		}
		for _, tc := range []struct {
			name          string
			versions      [][]urn.Tool
			deleteHighest bool
			excluded      bool
		}{
			{name: "platform_only", versions: [][]urn.Tool{{platform}}, excluded: true},
			{name: "mixed", versions: [][]urn.Tool{{ordinary, platform}}, excluded: true},
			{name: "ordinary", versions: [][]urn.Tool{{ordinary}}},
			{name: "empty", versions: [][]urn.Tool{{}}},
			{name: "no_version"},
			{name: "old_platform", versions: [][]urn.Tool{{platform}, {ordinary}}},
			{name: "latest_platform", versions: [][]urn.Tool{{ordinary}, {platform}}, excluded: true},
			{name: "deleted_highest_platform", versions: [][]urn.Tool{{ordinary}, {platform}}, deleteHighest: true},
			{name: "deleted_highest_ordinary", versions: [][]urn.Tool{{platform}, {ordinary}}, deleteHighest: true, excluded: true},
		} {
			for _, entrypoint := range []string{"audience", "role_grant", "setup", "eligible"} {
				t.Run(model+"/"+tc.name+"/"+entrypoint, func(t *testing.T) {
					t.Parallel()
					ctx, ti := newTestPluginsService(t)
					ac, _ := contextvalues.GetAuthContext(ctx)
					role := createTestRolePrincipal(t, ctx, ti, "distribution")
					params, toolsetID := platformDistributionServer(t, ctx, ti, wrapped)
					for i, tools := range tc.versions {
						_, err := toolsetsrepo.New(ti.conn).CreateToolsetVersion(ctx, toolsetsrepo.CreateToolsetVersionParams{ToolsetID: toolsetID, Version: int64(i + 1), ToolUrns: tools, ResourceUrns: []urn.Resource{}})
						require.NoError(t, err)
					}
					if tc.deleteHighest {
						//nolint:glint // notestingrawsql: No production API soft-deletes an individual historical version.
						_, err := ti.conn.Exec(ctx, "UPDATE toolset_versions SET deleted_at = clock_timestamp() WHERE toolset_id = $1 AND version = $2", toolsetID, len(tc.versions))
						require.NoError(t, err)
					}
					slug := "distribution"
					plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Distribution", Slug: &slug})
					require.NoError(t, err)
					assign := func() {
						_, err := ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{role}})
						require.NoError(t, err)
					}
					if entrypoint == "role_grant" || entrypoint == "eligible" {
						assign()
					}
					principal, err := urn.ParsePrincipal(role)
					require.NoError(t, err)
					selectors, err := authz.NewSelector(authz.ScopeMCPConnect, "*").MarshalJSON()
					require.NoError(t, err)
					_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: principal, Scope: string(authz.ScopeMCPConnect), Selectors: selectors})
					require.NoError(t, err)
					switch entrypoint {
					case "audience":
						assign()
					case "setup":
						require.NoError(t, processDirectRoleSetup(ctx, ti, role, plugins.PublicationRequests{}))
					case "role_grant", "eligible":
						tx, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: Exercise transactional distribution hooks.
						require.NoError(t, err)
						defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
						if entrypoint == "role_grant" {
							_, err = roledelivery.RoleChanged(ctx, tx, ac.ActiveOrganizationID, role, nil, nil)
						} else {
							_, err = plugins.AttachToDefaultAndRolePluginsAudited(ctx, tx, audit.NewLogger(), ac, params, nil)
						}
						require.NoError(t, err)
						require.NoError(t, tx.Commit(ctx))
					}
					got, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
					require.NoError(t, err)
					if tc.excluded {
						require.Empty(t, got.Servers)
					} else {
						require.Len(t, got.Servers, 1)
					}
					if entrypoint == "eligible" && tc.excluded {
						all, err := ti.service.ListPlugins(ctx, &gen.ListPluginsPayload{})
						require.NoError(t, err)
						for _, p := range all.Plugins {
							details, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: p.ID})
							require.NoError(t, err)
							require.Empty(t, details.Servers, "implicit Default attachment must also exclude platform tools")
						}
					}
					if tc.excluded {
						payload := &gen.AddPluginServerPayload{PluginID: plugin.ID, Policy: "required"}
						id := toolsetID.String()
						payload.ToolsetID = &id
						if wrapped {
							id = params.McpServerID.UUID.String()
							payload.ToolsetID = nil
							payload.McpServerID = &id
						}
						_, err := ti.service.AddPluginServer(ctx, payload)
						require.NoError(t, err, "explicit administrator membership remains allowed")
						got, err = ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
						require.NoError(t, err)
						require.Len(t, got.Servers, 1)
					}
				})
			}
		}
	}
}

func platformDistributionServer(t *testing.T, ctx context.Context, ti *testInstance, wrapped bool) (plugins.AttachToDefaultPluginParams, uuid.UUID) {
	t.Helper()
	ac, _ := contextvalues.GetAuthContext(ctx)
	params := plugins.AttachToDefaultPluginParams{OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, DisplayName: "Distribution server"}
	if wrapped {
		server := createTestMcpServer(t, ctx, ti.conn, "distribution", "private")
		params.McpServerID = uuid.NullUUID{UUID: server.id, Valid: true}
		backing, err := toolsetsrepo.New(ti.conn).GetToolset(ctx, toolsetsrepo.GetToolsetParams{ProjectID: *ac.ProjectID, Slug: server.backingToolsetSlug})
		require.NoError(t, err)
		return params, backing.ID
	}
	server := createTestToolset(t, ctx, ti.conn, "distribution")
	params.ToolsetID = uuid.NullUUID{UUID: server.ID, Valid: true}
	return params, server.ID
}

func TestPlatformToolsetDistributionAfterAssistantRemoval(t *testing.T) {
	t.Parallel()
	for _, wrapped := range []bool{false, true} {
		model := "direct"
		if wrapped {
			model = "wrapped"
		}
		for _, removal := range []string{"detach", "delete"} {
			t.Run(model+"/"+removal, func(t *testing.T) {
				t.Parallel()
				ctx, ti := newTestPluginsService(t)
				ac, _ := contextvalues.GetAuthContext(ctx)
				_, toolsetID := platformDistributionServer(t, ctx, ti, wrapped)
				_, err := toolsetsrepo.New(ti.conn).CreateToolsetVersion(ctx, toolsetsrepo.CreateToolsetVersionParams{
					ToolsetID: toolsetID, Version: 1,
					ToolUrns:     []urn.Tool{urn.NewTool(urn.ToolKindPlatform, "slack", "send_message")},
					ResourceUrns: []urn.Resource{},
				})
				require.NoError(t, err)
				q := assistantsrepo.New(ti.conn)
				assistant, err := q.CreateAssistant(ctx, assistantsrepo.CreateAssistantParams{
					ProjectID: *ac.ProjectID, OrganizationID: ac.ActiveOrganizationID,
					Name: "Distribution assistant", Model: "test-model", Instructions: "Test distribution", WarmTtlSeconds: 60, MaxConcurrency: 1, Status: "active",
				})
				require.NoError(t, err)
				_, err = q.AddAssistantToolsets(ctx, []assistantsrepo.AddAssistantToolsetsParams{{AssistantID: assistant.ID, ToolsetID: toolsetID, ProjectID: *ac.ProjectID}})
				require.NoError(t, err)
				role := createTestRolePrincipal(t, ctx, ti, "assistant-distribution")
				principal, err := urn.ParsePrincipal(role)
				require.NoError(t, err)
				selectors, err := authz.NewSelector(authz.ScopeMCPConnect, "*").MarshalJSON()
				require.NoError(t, err)
				_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: principal, Scope: string(authz.ScopeMCPConnect), Selectors: selectors})
				require.NoError(t, err)
				assertExcluded := func(name string) {
					plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: name})
					require.NoError(t, err)
					_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{role}})
					require.NoError(t, err)
					got, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
					require.NoError(t, err)
					require.Empty(t, got.Servers)
				}
				assertExcluded("Before assistant removal")
				if removal == "detach" {
					require.NoError(t, q.ClearAssistantToolsets(ctx, assistantsrepo.ClearAssistantToolsetsParams{AssistantID: assistant.ID, ProjectID: *ac.ProjectID}))
				} else {
					require.NoError(t, q.DeleteAssistant(ctx, assistantsrepo.DeleteAssistantParams{AssistantID: assistant.ID, ProjectID: *ac.ProjectID}))
				}
				assertExcluded("After assistant removal")
			})
		}
	}
}
