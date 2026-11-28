package mcpendpoints_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_endpoints"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestResolveDeniesUnservedOrganization(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		meta        bool
		whitelisted bool
		disabled    bool
		allowed     bool
	}{
		{name: "served", whitelisted: true, disabled: false, allowed: true},
		{name: "not_whitelisted", whitelisted: false, disabled: false, allowed: false},
		{name: "disabled", whitelisted: true, disabled: true, allowed: false},
		{name: "disabled_not_whitelisted", whitelisted: false, disabled: true, allowed: false},
		{name: "meta_served", meta: true, whitelisted: true, disabled: false, allowed: true},
		{name: "meta_not_whitelisted", meta: true, whitelisted: false, disabled: false, allowed: false},
		{name: "meta_disabled", meta: true, whitelisted: true, disabled: true, allowed: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)

			payload := &gen.CreateMcpEndpointPayload{
				SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
				CustomDomainID: nil, McpServerID: nil, MetaMcpServerID: nil,
				Slug: types.McpEndpointSlug(authCtx.OrganizationSlug + "-org-" + tc.name),
			}
			if tc.meta {
				payload.MetaMcpServerID = conv.PtrEmpty(seedMetaMcpServerWithMode(t, ctx, ti, *authCtx.ProjectID, networkaccess.ModePublicOnly).String())
			} else {
				payload.McpServerID = conv.PtrEmpty(seedMcpServerWithMode(t, ctx, ti.conn, *authCtx.ProjectID, "public", networkaccess.ModePublicOnly).String())
			}
			_, err := ti.service.CreateMcpEndpoint(ctx, payload)
			require.NoError(t, err)

			err = testrepo.New(ti.conn).SetOrganizationServingStateFixture(ctx, testrepo.SetOrganizationServingStateFixtureParams{
				ID: authCtx.ActiveOrganizationID, Whitelisted: tc.whitelisted, Disabled: tc.disabled,
			})
			require.NoError(t, err)

			result, err := mcpendpoints.Resolve(ctx, ti.conn, testenv.NewLogger(t), mcpendpoints.ResolutionInput{
				Slug:                 string(payload.Slug),
				NamespaceKind:        mcpendpoints.NamespacePlatform,
				CustomDomainID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
				ExpectedOrganization: authCtx.ActiveOrganizationID,
				Surface:              networkaccess.SurfacePublic,
			})
			require.NoError(t, err)
			require.True(t, result.Found, "an unserved organization's endpoint is an authoritative denial, not a miss")
			require.Equal(t, tc.allowed, result.Allowed)
		})
	}
}
