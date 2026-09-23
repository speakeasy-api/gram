package plugins_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestGatewayPluginAttachmentFailsClosedWithoutGate(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Gateway gate"})
	require.NoError(t, err)

	gatewayID := uuid.NewString()
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID: plugin.ID, MetaMcpServerID: &gatewayID, Policy: "required",
	})
	var gateErr *oops.ShareableError
	require.ErrorAs(t, err, &gateErr)
	require.Equal(t, oops.CodeUnavailable, gateErr.Code)

	toolset := createTestToolset(t, ctx, ti.conn, "gateway-gate-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID: plugin.ID, ToolsetID: conv.PtrEmpty(toolset.ID.String()), MetaMcpServerID: &gatewayID, Policy: "required",
	})
	var backendErr *oops.ShareableError
	require.ErrorAs(t, err, &backendErr)
	require.Equal(t, oops.CodeBadRequest, backendErr.Code)
}

func TestGatewayPluginAttachmentWithEnabledGate(t *testing.T) {
	t.Parallel()
	features := &feature.InMemory{}
	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHubAndFeatures(t, mock, features)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	features.SetFlag(feature.FlagGatewayPluginMembership, ac.ActiveOrganizationID, true)
	features.SetFlag(feature.FlagPlatformMCPShadowAudienceEnforcement, ac.ActiveOrganizationID, false)
	features.SetFlag(feature.FlagPlatformMCPDirectRemoteDistributionDisabled, ac.ActiveOrganizationID, false)
	ti.service.WithDistributionAdmission(admission.NewGuard(features, nil))

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Gateway member"})
	require.NoError(t, err)
	issuer, err := usersessionsrepo.New(ti.conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID: *ac.ProjectID, OrganizationID: conv.ToPGText(ac.ActiveOrganizationID), Slug: "gateway-issuer",
		AuthnChallengeMode: "interactive", SessionDuration: pgtype.Interval{Microseconds: time.Hour.Microseconds(), Valid: true},
	})
	require.NoError(t, err)
	gateway, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, Name: "Gateway", Visibility: "private",
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true}, NetworkAccessMode: pgtype.Text{},
	})
	require.NoError(t, err)
	_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: *ac.ProjectID, MetaMcpServerID: uuid.NullUUID{UUID: gateway.ID, Valid: true}, Slug: "gateway-test",
	})
	require.NoError(t, err)

	attached, err := ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID: plugin.ID, MetaMcpServerID: conv.PtrEmpty(gateway.ID.String()), Policy: "required",
	})
	require.NoError(t, err)
	require.Equal(t, gateway.ID.String(), *attached.MetaMcpServerID)
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID: plugin.ID, MetaMcpServerID: conv.PtrEmpty(gateway.ID.String()), Policy: "required",
	})
	var duplicate *oops.ShareableError
	require.ErrorAs(t, err, &duplicate)
	require.Equal(t, oops.CodeConflict, duplicate.Code)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	var config struct {
		MCPServers map[string]struct {
			URL string `json:"url"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(mock.lastPushedFiles[plugin.Slug+"/.mcp.json"], &config))
	require.Equal(t, "https://app.getgram.ai/mcp/gateway-test", config.MCPServers["Gateway"].URL)

	fixtures := testrepo.New(ti.conn)
	_, err = fixtures.SetMetaMCPServerNetworkAccessModeFixture(ctx, testrepo.SetMetaMCPServerNetworkAccessModeFixtureParams{
		ID: gateway.ID, OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID,
		NetworkAccessMode: pgtype.Text{String: "private_only", Valid: true},
	})
	require.NoError(t, err)
	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.ErrorContains(t, errors.Unwrap(err), "no endpoint in the private ingress namespace")
	require.NoError(t, fixtures.InsertNetworkIngressFixture(ctx, testrepo.InsertNetworkIngressFixtureParams{
		ID: uuid.New(), OrganizationID: ac.ActiveOrganizationID, DnsName: pgtype.Text{String: "tail.example", Valid: true},
	}))
	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(mock.lastPushedFiles[plugin.Slug+"/.mcp.json"], &config))
	require.Equal(t, "https://tail.example/mcp/gateway-test", config.MCPServers["Gateway"].URL)

	ti.service.WithDistributionAdmission(nil)
	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	var unavailable *oops.ShareableError
	require.ErrorAs(t, err, &unavailable)
	require.Equal(t, oops.CodeUnavailable, unavailable.Code)

	features.SetFlag(feature.FlagGatewayPluginMembership, ac.ActiveOrganizationID, false)
	err = ti.service.RemovePluginServer(ctx, &gen.RemovePluginServerPayload{PluginID: plugin.ID, ID: attached.ID})
	require.NoError(t, err)
}

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

			if allowed {
				// Exercise the real writer grant set, without adding org:read.
				listed, err := ti.service.ListPlugins(restricted, &gen.ListPluginsPayload{})
				require.NoError(t, err)
				require.Len(t, listed.Plugins, 1)
				_, err = ti.service.GetPlugin(restricted, &gen.GetPluginPayload{ID: listed.Plugins[0].ID})
				require.NoError(t, err)
			}

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
