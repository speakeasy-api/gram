package plugins_test

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/stretchr/testify/require"
)

func TestPluginReadAuthorization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Read authorization"})
	require.NoError(t, err)
	writer := authz.NewGrant(authz.ScopePluginWrite, ac.ProjectID.String())
	reader := authz.NewGrant(authz.ScopeOrgRead, ac.ActiveOrganizationID)
	admin := authz.NewGrant(authz.ScopeOrgAdmin, ac.ActiveOrganizationID)
	blocked := authz.NewGrant(authz.ScopePluginBlockedWrite, ac.ProjectID.String())
	for _, tc := range []struct {
		name    string
		grants  []authz.Grant
		allowed bool
	}{
		{"writer_without_org_scope", []authz.Grant{writer}, true},
		{"writer_with_skill_and_mcp", []authz.Grant{writer, authz.NewGrant(authz.ScopeSkillWrite, "*"), authz.NewGrant(authz.ScopeMCPWrite, "*")}, true},
		{"org_reader", []authz.Grant{reader}, true},
		{"org_admin", []authz.Grant{admin}, true},
		{"blocked_writer", []authz.Grant{writer, blocked}, false},
		{"reader_with_blocked_writer", []authz.Grant{reader, writer, blocked}, true},
		{"admin_with_blocked_writer", []authz.Grant{admin, blocked}, true},
		{"wrong_project", []authz.Grant{authz.NewGrant(authz.ScopePluginWrite, uuid.NewString())}, false},
		{"wrong_org_reader", []authz.Grant{authz.NewGrant(authz.ScopeOrgRead, "org_other")}, false},
		{"wrong_org_admin", []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, "org_other")}, false},
		{"no_grants", nil, false},
		{"skill_only", []authz.Grant{authz.NewGrant(authz.ScopeSkillWrite, "*")}, false},
		{"mcp_only", []authz.Grant{authz.NewGrant(authz.ScopeMCPWrite, "*")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			restricted := authztest.WithExactGrants(t, ctx, tc.grants...)
			for _, op := range []struct {
				name string
				run  func(*testing.T) error
			}{
				{"list", func(t *testing.T) error {
					t.Helper()
					result, err := ti.service.ListPlugins(restricted, &gen.ListPluginsPayload{})
					if err == nil {
						ids := []string{}
						for _, p := range result.Plugins {
							ids = append(ids, p.ID)
						}
						require.Contains(t, ids, plugin.ID)
					}
					if err != nil {
						return fmt.Errorf("list: %w", err)
					}
					return nil
				}},
				{"get", func(t *testing.T) error {
					t.Helper()
					result, err := ti.service.GetPlugin(restricted, &gen.GetPluginPayload{ID: plugin.ID})
					if err == nil {
						require.Equal(t, plugin.ID, result.ID)
					}
					if err != nil {
						return fmt.Errorf("get: %w", err)
					}
					return nil
				}},
				{"download", func(t *testing.T) error {
					t.Helper()
					result, body, err := ti.service.DownloadPluginPackage(restricted, &gen.DownloadPluginPackagePayload{PluginID: plugin.ID, Platform: "claude"})
					if body != nil {
						defer func() { require.NoError(t, body.Close()) }()
					}
					if err == nil {
						require.Equal(t, "application/zip", result.ContentType)
						data, readErr := io.ReadAll(body)
						require.NoError(t, readErr)
						require.NotEmpty(t, data)
					}
					if err != nil {
						return fmt.Errorf("download: %w", err)
					}
					return nil
				}},
				{"publish_status", func(t *testing.T) error {
					t.Helper()
					result, err := ti.service.GetPublishStatus(restricted, &gen.GetPublishStatusPayload{})
					if err == nil {
						require.NotNil(t, result)
					}
					if err != nil {
						return fmt.Errorf("publish_status: %w", err)
					}
					return nil
				}},
			} {
				t.Run(op.name, func(t *testing.T) {
					t.Parallel()
					err := op.run(t)
					if tc.allowed {
						require.NoError(t, err)
					} else {
						requireProjectionOopsCode(t, err, oops.CodeForbidden)
					}
				})
			}
		})
	}
}

//nolint:paralleltest // Updates deliberately share a plugin with the read assertions.
func TestPluginAssignmentMetadataIsAdminOnly(t *testing.T) {
	ctx, ti := newTestPluginsService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Private audience"})
	require.NoError(t, err)
	principal := createTestRolePrincipal(t, ctx, ti, "private-audience")
	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{principal}})
	require.NoError(t, err)
	for _, tc := range []struct {
		name         string
		grants       []authz.Grant
		admin, write bool
	}{
		{"writer", []authz.Grant{authz.NewGrant(authz.ScopePluginWrite, ac.ProjectID.String())}, false, true},
		{"reader", []authz.Grant{authz.NewGrant(authz.ScopeOrgRead, ac.ActiveOrganizationID)}, false, false},
		{"reader_blocked_writer", []authz.Grant{authz.NewGrant(authz.ScopeOrgRead, ac.ActiveOrganizationID), authz.NewGrant(authz.ScopePluginWrite, ac.ProjectID.String()), authz.NewGrant(authz.ScopePluginBlockedWrite, ac.ProjectID.String())}, false, false},
		{"admin", []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, ac.ActiveOrganizationID)}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			restricted := authztest.WithExactGrants(t, ctx, tc.grants...)
			t.Run("list", func(t *testing.T) {
				result, err := ti.service.ListPlugins(restricted, &gen.ListPluginsPayload{})
				require.NoError(t, err)
				var found *gen.Plugin
				for _, p := range result.Plugins {
					if p.ID == plugin.ID {
						found = p
					}
				}
				require.NotNil(t, found)
				if tc.admin {
					require.NotNil(t, found.AssignmentCount)
					require.EqualValues(t, 1, *found.AssignmentCount)
				} else {
					require.Nil(t, found.AssignmentCount)
					require.Nil(t, found.Assignments)
				}
			})
			t.Run("get", func(t *testing.T) {
				result, err := ti.service.GetPlugin(restricted, &gen.GetPluginPayload{ID: plugin.ID})
				require.NoError(t, err)
				if tc.admin {
					require.Len(t, result.Assignments, 1)
					require.Equal(t, principal, result.Assignments[0].PrincipalUrn)
				} else {
					require.Nil(t, result.Assignments)
					require.Nil(t, result.AssignmentCount)
				}
			})
			t.Run("update", func(t *testing.T) {
				result, err := ti.service.UpdatePlugin(restricted, &gen.UpdatePluginPayload{ID: plugin.ID, Name: "Edited audience", Slug: "edited-audience"})
				if !tc.write {
					requireProjectionOopsCode(t, err, oops.CodeForbidden)
					return
				}
				require.NoError(t, err)
				if tc.admin {
					require.Len(t, result.Assignments, 1)
					require.Equal(t, principal, result.Assignments[0].PrincipalUrn)
				} else {
					require.Nil(t, result.Assignments)
					require.Nil(t, result.AssignmentCount)
				}
			})
		})
	}
}

func TestPluginWriterCannotManageMarketplaceOrAssignments(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Administrative boundary"})
	require.NoError(t, err)
	writer := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopePluginWrite, ac.ProjectID.String()))
	for _, op := range []struct {
		name string
		run  func(*testing.T) error
	}{
		{"get_marketplace", func(t *testing.T) error {
			t.Helper()
			_, err := ti.service.GetMarketplaceSettings(writer, &gen.GetMarketplaceSettingsPayload{})
			if err != nil {
				return fmt.Errorf("get_marketplace: %w", err)
			}
			return nil
		}},
		{"update_marketplace", func(t *testing.T) error {
			t.Helper()
			name := "writer-marketplace"
			_, err := ti.service.UpdateMarketplaceSettings(writer, &gen.UpdateMarketplaceSettingsPayload{MarketplaceName: &name})
			if err != nil {
				return fmt.Errorf("update_marketplace: %w", err)
			}
			return nil
		}},
		{"list_audiences", func(t *testing.T) error {
			t.Helper()
			_, err := ti.service.ListAudiences(writer, &gen.ListAudiencesPayload{})
			if err != nil {
				return fmt.Errorf("list_audiences: %w", err)
			}
			return nil
		}},
		{"download_observability", func(t *testing.T) error {
			t.Helper()
			_, body, err := ti.service.DownloadObservabilityPlugin(writer, &gen.DownloadObservabilityPluginPayload{})
			if body != nil {
				defer func() { require.NoError(t, body.Close()) }()
			}
			if err != nil {
				return fmt.Errorf("download_observability: %w", err)
			}
			return nil
		}},
		{"download_codex_install_script", func(t *testing.T) error {
			t.Helper()
			_, body, err := ti.service.DownloadCodexInstallScript(writer, &gen.DownloadCodexInstallScriptPayload{})
			if body != nil {
				defer func() { require.NoError(t, body.Close()) }()
			}
			if err != nil {
				return fmt.Errorf("download_codex_install_script: %w", err)
			}
			return nil
		}},
		{"set_assignments", func(t *testing.T) error {
			t.Helper()
			_, err := ti.service.SetPluginAssignments(writer, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{"*"}})
			if err != nil {
				return fmt.Errorf("set_assignments: %w", err)
			}
			return nil
		}},
	} {
		t.Run(op.name, func(t *testing.T) { t.Parallel(); requireProjectionOopsCode(t, op.run(t), oops.CodeForbidden) })
	}
}

func TestPluginReadsAreTenantScoped(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	foreignProject, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Other project", Slug: "other-project", OrganizationID: ac.ActiveOrganizationID})
	require.NoError(t, err)
	foreign := *ac
	foreign.ProjectID = &foreignProject.ID
	foreignCtx := contextvalues.SetAuthContext(ctx, &foreign)
	foreignCtx = authztest.WithExactGrants(t, foreignCtx, authz.NewGrant(authz.ScopeOrgAdmin, ac.ActiveOrganizationID))
	plugin, err := ti.service.CreatePlugin(foreignCtx, &gen.CreatePluginPayload{Name: "Foreign plugin"})
	require.NoError(t, err)
	for _, tc := range []struct {
		name    string
		context func() context.Context
	}{
		{"same_org_other_project_writer", func() context.Context {
			return authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopePluginWrite, ac.ProjectID.String()))
		}},
		{"same_org_other_project_admin", func() context.Context {
			return authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ac.ActiveOrganizationID))
		}},
		{"other_org_same_project", func() context.Context {
			other := foreign
			other.ActiveOrganizationID = "org_other"
			c := contextvalues.SetAuthContext(ctx, &other)
			return authztest.WithExactGrants(t, c, authz.NewGrant(authz.ScopeOrgRead, other.ActiveOrganizationID))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			restricted := tc.context()
			t.Run("get", func(t *testing.T) {
				t.Parallel()
				_, err := ti.service.GetPlugin(restricted, &gen.GetPluginPayload{ID: plugin.ID})
				requireProjectionOopsCode(t, err, oops.CodeNotFound)
			})
			t.Run("download", func(t *testing.T) {
				t.Parallel()
				_, body, err := ti.service.DownloadPluginPackage(restricted, &gen.DownloadPluginPackagePayload{PluginID: plugin.ID, Platform: "claude"})
				if body != nil {
					defer func() { require.NoError(t, body.Close()) }()
				}
				requireProjectionOopsCode(t, err, oops.CodeNotFound)
			})
			t.Run("list", func(t *testing.T) {
				t.Parallel()
				result, err := ti.service.ListPlugins(restricted, &gen.ListPluginsPayload{})
				require.NoError(t, err)
				for _, p := range result.Plugins {
					require.NotEqual(t, plugin.ID, p.ID)
				}
			})
		})
	}
}

//nolint:paralleltest // The existing publisher fixture records mutable state.
func TestPluginWriterPublishesWithoutExposingAdministration(t *testing.T) {
	publisher := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, publisher)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	writer := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopePluginWrite, ac.ProjectID.String()))
	plugin, err := ti.service.CreatePlugin(writer, &gen.CreatePluginPayload{Name: "Writer publish"})
	require.NoError(t, err)
	toolset := createTestToolset(t, ctx, ti.conn, "writer-publish-server")
	toolsetID := toolset.ID.String()
	_, err = ti.service.AddPluginServer(writer, &gen.AddPluginServerPayload{PluginID: plugin.ID, ToolsetID: &toolsetID, Policy: "required"})
	require.NoError(t, err)
	published, err := ti.service.PublishPlugins(writer, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	require.NotEmpty(t, published.RepoURL)
	require.True(t, publisher.pushFilesCalled)
	require.NotEmpty(t, publisher.lastPushedFiles)
	admin := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ac.ActiveOrganizationID))
	baseline, err := ti.service.GetPublishStatus(admin, &gen.GetPublishStatusPayload{})
	require.NoError(t, err)
	require.NotNil(t, baseline.MarketplaceURL)
	require.Contains(t, *baseline.MarketplaceURL, "/marketplace/")
	require.NotNil(t, baseline.HasCollaborators)
	require.NotNil(t, baseline.LiveVersion)
	for _, tc := range []struct {
		name   string
		grants []authz.Grant
	}{
		{"writer", []authz.Grant{authz.NewGrant(authz.ScopePluginWrite, ac.ProjectID.String())}},
		{"reader", []authz.Grant{authz.NewGrant(authz.ScopeOrgRead, ac.ActiveOrganizationID)}},
		{"reader_blocked_writer", []authz.Grant{authz.NewGrant(authz.ScopeOrgRead, ac.ActiveOrganizationID), authz.NewGrant(authz.ScopePluginBlockedWrite, ac.ProjectID.String())}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			restricted := authztest.WithExactGrants(t, ctx, tc.grants...)
			status, err := ti.service.GetPublishStatus(restricted, &gen.GetPublishStatusPayload{})
			require.NoError(t, err)
			require.True(t, status.Configured)
			require.True(t, status.Connected)
			require.Equal(t, baseline.RepoURL, status.RepoURL)
			// Publisher fixtures share a mock repository version cache; verify visibility.
			require.NotNil(t, status.LiveVersion)
			require.NotEmpty(t, *status.LiveVersion)
			require.Equal(t, baseline.LastPublishedAt, status.LastPublishedAt)
			require.Equal(t, baseline.UpToDate, status.UpToDate)
			require.Nil(t, status.MarketplaceURL, "bearer install URL is administrator-only")
			require.Nil(t, status.HasCollaborators, "collaborator metadata is administrator-only")
		})
	}
}
