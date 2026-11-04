package mcp_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/builtinmcp"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	authclientfixtures "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
)

func TestBuiltinConsentLocalInventoryAndCredentialBinding(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"enforced", "off", "shadow"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ctx, fx := standaloneConsent(t, "builtin-consent", "", func(ctx context.Context, ti *testInstance, projectID, issuerID uuid.UUID, _ string) uuid.UUID {
				ac, ok := contextvalues.GetAuthContext(ctx)
				require.True(t, ok)
				require.NoError(t, builtinmcp.EnsureServer(ctx, ti.conn, ac.ActiveOrganizationID, projectID, builtinmcp.Slack, conv.ToNullUUID(issuerID)))
				return builtinmcp.ID(projectID, builtinmcp.Slack)
			})
			fx.ti.service.manual = true
			fx.ti.service.SetAuthClientPermitted(true)
			seedUserMCPConnectGrant(t, ctx, fx.ti.conn, fx.orgID, fx.subject.ID, "*")
			if mode == "enforced" {
				_, err := productfeaturesrepo.New(fx.ti.conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: fx.orgID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
				require.NoError(t, err)
			} else {
				fx.ti.features.SetFlag(feature.FlagAuthClientResolver, fx.orgID, false)
				fx.ti.features.SetFlag(feature.FlagAuthClientResolverShadow, fx.orgID, mode == "shadow")
			}
			fixtures := authclientfixtures.New(fx.ti.conn)
			addClient := func() uuid.UUID {
				id, err := fixtures.SeedAuthClientFixture(ctx, authclientfixtures.SeedAuthClientFixtureParams{OrganizationID: fx.orgID, ProjectID: conv.ToNullUUID(fx.projectID), ClientType: "static_credential", Source: "admin"})
				require.NoError(t, err)
				require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientfixtures.SeedServerAuthClientLinkFixtureParams{ProjectID: fx.projectID, OrganizationID: fx.orgID, McpServerID: fx.endpoint.McpServerID.UUID, AuthClientID: id, Source: "admin"}))
				require.NoError(t, fixtures.SeedAuthClientHeaderFixture(ctx, authclientfixtures.SeedAuthClientHeaderFixtureParams{AuthClientID: id, OrganizationID: fx.orgID, Name: "Authorization", IsRequired: true, Value: conv.ToPGText("Bearer fixture-token")}))
				return id
			}
			first, second := addClient(), addClient()
			attempt := uuid.NewString()
			preview := serveConsentMCPRequest(t, ctx, fx.ti, fx.endpoint, fx.stateID, "csrf-token", attempt, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, map[string]string{"Gram-Consent-Auth-Client": first.String()})
			// No upstream server or Slack credential environment exists. Inventory
			// must be entirely local and must never echo credential values.
			definition, ok := builtinmcp.Lookup(builtinmcp.Slack)
			require.True(t, ok)
			for _, tool := range definition.Tools {
				require.Contains(t, preview.Body.String(), tool.Name)
			}
			require.NotContains(t, preview.Body.String(), "fixture-token")
			if mode != "enforced" {
				return
			}
			_, err := approveDirectScope(ctx, fx, second, attempt)
			var conflict *oops.ShareableError
			require.ErrorAs(t, err, &conflict)
			require.Equal(t, oops.CodeFailedPrecondition, conflict.Code)
			_, err = fx.ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
			require.NoError(t, err, "changed credentials require preview, not challenge consumption")
			attempt = previewDirectScope(t, ctx, fx, second)
			approved, err := approveDirectScope(ctx, fx, second, attempt)
			require.NoError(t, err)
			require.Equal(t, http.StatusSeeOther, approved.Code)
			location, err := url.Parse(approved.Header().Get("Location"))
			require.NoError(t, err)
			grants := cache.NewTypedObjectCache[mcp.UserSessionGrant](fx.ti.logger, fx.ti.cacheAdapter, cache.SuffixNone)
			grant, err := grants.Get(ctx, "userSessionGrant:"+fx.shared.String()+":"+location.Query().Get("code"))
			require.NoError(t, err)
			require.Equal(t, map[uuid.UUID]uuid.UUID{fx.endpoint.McpServerID.UUID: second}, grant.AuthClientPicks)
			require.NotNil(t, grant.ToolSelection)
			require.Equal(t, "mcp_server:"+fx.endpoint.McpServerID.UUID.String(), grant.ToolSelection.Resource)
		})
	}
}
