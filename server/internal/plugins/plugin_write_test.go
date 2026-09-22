package plugins_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

//nolint:paralleltest // Operations mutate and delete the same plugin and server, so subtests must run sequentially.
func TestPluginWriteAuthorization(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"plugin_write", "org_admin", "skill_write", "wrong_project", "no_grants", "blocked_admin", "blocked_writer"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestPluginsService(t)
			ac, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			writer := authz.NewGrant(authz.ScopePluginWrite, ac.ProjectID.String())
			admin := authz.NewGrant(authz.ScopeOrgAdmin, ac.ActiveOrganizationID)
			blocked := authz.NewGrant(authz.ScopePluginBlockedWrite, ac.ProjectID.String())
			grants := map[string][]authz.Grant{
				"plugin_write":   {writer},
				"org_admin":      {admin},
				"skill_write":    {authz.NewGrant(authz.ScopeSkillWrite, ac.ProjectID.String())},
				"wrong_project":  {authz.NewGrant(authz.ScopePluginWrite, uuid.NewString())},
				"no_grants":      {},
				"blocked_admin":  {admin, writer, blocked},
				"blocked_writer": {writer, blocked},
			}[name]
			allowed := name == "plugin_write" || name == "org_admin"
			restricted := authztest.WithExactGrants(t, ctx, grants...)

			// Read permission must not let a denied writer provision the default plugin.
			readCtx := authztest.WithExactGrants(t, ctx, append(grants, authz.NewGrant(authz.ScopeOrgRead, ac.ActiveOrganizationID))...)
			listed, err := ti.service.ListPlugins(readCtx, &gen.ListPluginsPayload{})
			require.NoError(t, err)
			if allowed {
				require.Len(t, listed.Plugins, 1)
				require.NotNil(t, listed.Plugins[0].IsDefault)
				require.True(t, *listed.Plugins[0].IsDefault)
			} else {
				require.Empty(t, listed.Plugins)
			}

			plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Write authorization fixture"})
			require.NoError(t, err)
			toolset := createTestToolset(t, ctx, ti.conn, "write-authz-existing")
			server, err := ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
				PluginID: plugin.ID, ToolsetID: conv.PtrEmpty(toolset.ID.String()), Policy: "required",
			})
			require.NoError(t, err)
			anotherToolset := createTestToolset(t, ctx, ti.conn, "write-authz-new")

			operations := []struct {
				name string
				run  func(context.Context) error
			}{
				{"create", func(ctx context.Context) error {
					_, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Authorized content"})
					if err != nil {
						return fmt.Errorf("plugin write operation: %w", err)
					}
					return nil
				}},
				{"update", func(ctx context.Context) error {
					_, err := ti.service.UpdatePlugin(ctx, &gen.UpdatePluginPayload{ID: plugin.ID, Name: "Updated content", Slug: "updated-content"})
					if err != nil {
						return fmt.Errorf("plugin write operation: %w", err)
					}
					return nil
				}},
				{"add_server", func(ctx context.Context) error {
					_, err := ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, ToolsetID: conv.PtrEmpty(anotherToolset.ID.String()), Policy: "required"})
					if err != nil {
						return fmt.Errorf("plugin write operation: %w", err)
					}
					return nil
				}},
				{"update_server", func(ctx context.Context) error {
					_, err := ti.service.UpdatePluginServer(ctx, &gen.UpdatePluginServerPayload{ID: server.ID, PluginID: plugin.ID, DisplayName: "Updated reference", Policy: "required"})
					if err != nil {
						return fmt.Errorf("plugin write operation: %w", err)
					}
					return nil
				}},
				{"remove_server", func(ctx context.Context) error {
					return ti.service.RemovePluginServer(ctx, &gen.RemovePluginServerPayload{ID: server.ID, PluginID: plugin.ID})
				}},
				{"delete", func(ctx context.Context) error {
					return ti.service.DeletePlugin(ctx, &gen.DeletePluginPayload{ID: plugin.ID})
				}},
			}
			for _, operation := range operations {
				t.Run(operation.name, func(t *testing.T) {
					err := operation.run(restricted)
					if allowed {
						require.NoError(t, err)
					} else {
						requireProjectionOopsCode(t, err, oops.CodeForbidden)
					}
				})
			}
			t.Run("publish", func(t *testing.T) {
				_, err := ti.service.PublishPlugins(restricted, &gen.PublishPluginsPayload{})
				if allowed {
					// This fixture intentionally has no GitHub publisher: reaching its
					// configuration error proves authorization passed without publishing.
					requireProjectionOopsCode(t, err, oops.CodeBadRequest)
					require.ErrorContains(t, err, "GitHub publishing is not configured")
				} else {
					requireProjectionOopsCode(t, err, oops.CodeForbidden)
				}
			})
		})
	}
}
