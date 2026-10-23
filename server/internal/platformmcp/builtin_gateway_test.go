package platformmcp

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestBuiltinGatewayToolContracts(t *testing.T) {
	t.Parallel()
	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "live", Version: "1"}, nil))
	absent := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "absent", Version: "1"}, nil))
	registerBuiltinGatewayTools(live, &GatewayService{})
	registerBuiltinGatewayTools(absent, nil)
	require.Len(t, live.Descriptors(), 2)
	require.Empty(t, live.For(AudienceAssistant))
	for i, d := range live.Descriptors() {
		unavailable := absent.Descriptors()[i]
		require.Equal(t, ExternalAuthorizationOrgAdmin, d.Meta.Authorization)
		require.Equal(t, ProjectScopeExplicit, d.Meta.ProjectScope)
		require.Equal(t, externalOnly, d.Meta.Audiences)
		require.Equal(t, d.Meta, unavailable.Meta)
		require.JSONEq(t, string(d.InputSchema), string(unavailable.InputSchema))
		require.Equal(t, d.output, unavailable.output)
		require.Equal(t, d.Annotations, unavailable.Annotations)
		args := map[string]any{"project_id": uuid.NewString(), "gateway_id": uuid.NewString()}
		if d.Name == "configure_builtin_gateway_member" {
			args["key"] = "slack"
			args["present"] = true
			args["sort_order"] = 0
			args["expected_version"] = "version"
			args["confirmed"] = true
		}
		raw, err := json.Marshal(args)
		require.NoError(t, err)
		_, err = unavailable.Invoke(contextWithPrincipal(t.Context(), Principal{OrganizationID: "org_example", UserID: "user_example"}), raw)
		var refusal *ToolRefusalError
		require.ErrorAs(t, err, &refusal)
		require.Contains(t, refusal.Payload, unavailableCode)
	}
}

func TestBuiltinGatewayMembershipRoundTrip(t *testing.T) {
	t.Parallel()
	ctx, f := seedGatewayFixture(t, "builtin_gateway_roundtrip")
	regular := f.seedRemoteMember(t, ctx, "Regular")
	gateway, err := f.create(ctx, "Built-in Gateway", regular)
	require.NoError(t, err)
	ctx = f.grantMCP(ctx, f.project.ID, uuid.MustParse(gateway.Gateway.ID))
	input := GetBuiltinGatewayMembersInput{ProjectID: f.project.ID.String(), GatewayID: gateway.Gateway.ID}
	before, err := f.gateways.builtinGatewayMembers(ctx, f.principal, input, nil)
	require.NoError(t, err)
	require.Empty(t, before.Members)
	change := ConfigureBuiltinGatewayMemberInput{ProjectID: input.ProjectID, GatewayID: input.GatewayID, Key: "slack", Present: true, ExpectedVersion: before.Version, Confirmed: true, SortOrder: 4}
	unconfirmed := change
	unconfirmed.Confirmed = false
	_, err = f.gateways.builtinGatewayMembers(ctx, f.principal, input, &unconfirmed)
	require.ErrorIs(t, err, errBuiltinMCPInvalid)
	added, err := f.gateways.builtinGatewayMembers(ctx, f.principal, input, &change)
	require.NoError(t, err)
	require.Len(t, added.Members, 1)
	require.Equal(t, "slack", added.Members[0].Key)
	require.EqualValues(t, 4, added.Members[0].SortOrder)
	require.NotEqual(t, before.Version, added.Version)
	_, err = f.gateways.builtinGatewayMembers(ctx, f.principal, input, &change)
	var stale *oops.ShareableError
	require.ErrorAs(t, err, &stale)
	require.Equal(t, oops.CodeConflict, stale.Code)
	// Editing regular membership must not silently remove built-ins.
	_, err = f.set(ctx, gateway.Gateway.ID, regular)
	require.NoError(t, err)
	unchanged, err := f.gateways.builtinGatewayMembers(ctx, f.principal, input, nil)
	require.NoError(t, err)
	require.Equal(t, added, unchanged)
	change.ExpectedVersion = added.Version
	change.Present = false
	removed, err := f.gateways.builtinGatewayMembers(ctx, f.principal, input, &change)
	require.NoError(t, err)
	require.Empty(t, removed.Members)
	regularView, err := f.gateways.readGateway(ctx, f.principal, f.project.ID, uuid.MustParse(gateway.Gateway.ID))
	require.NoError(t, err)
	require.Len(t, regularView.Members, 1)
	require.Equal(t, regular.String(), regularView.Members[0].MCPID)
}
