package platformmcp

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/feature"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
)

type tunneledSetupHarness struct {
	fixture tunnelStatusFixture
	limiter *recordingOperationLimiter
	flags   *feature.InMemory
	tool    Descriptor
	orgSlug string
}

// newTunneledSetupHarness returns the harness and the caller's context: the
// principal bound to the grants its live organization membership prepared.
func newTunneledSetupHarness(t *testing.T, fixture tunnelStatusFixture, dashboardURL string, limiter *recordingOperationLimiter, tunnelsEnabled bool) (tunneledSetupHarness, context.Context) {
	t.Helper()
	engine := authz.NewEngine(testenv.NewLogger(t), fixture.conn, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient())
	prepared, err := NewLiveOrgAdminAuthorizer(fixture.conn, engine).PrepareExternalContext(t.Context(), fixture.principal)
	require.NoError(t, err)
	origin, err := url.Parse(dashboardURL)
	require.NoError(t, err)
	// Without tunnelsEnabled the provider holds no decision for the
	// organization, which evaluates as indeterminate.
	flags := &feature.InMemory{}
	if tunnelsEnabled {
		flags.SetFlag(feature.FlagTunneledMCP, fixture.principal.OrganizationID, true)
	}
	reader := NewPostgresReader(testenv.NewLogger(t), fixture.conn).
		WithAuthorization(engine).
		WithTunneledMCPSetupHandoff(origin, OperationBudget{Connection: limiter, Organization: limiter}, flags)
	require.NotNil(t, reader.tunneledSetup)

	registrar := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil))
	registerTunneledMCPSetupHandoffTool(registrar, reader.tunneledSetup)

	organization, err := organizationsrepo.New(fixture.conn).GetOrganizationMetadata(t.Context(), fixture.principal.OrganizationID)
	require.NoError(t, err)
	return tunneledSetupHarness{
		fixture: fixture,
		limiter: limiter,
		flags:   flags,
		tool:    descriptorByName(t, registrar, getTunneledMCPSetupHandoffToolName),
		orgSlug: organization.Slug,
	}, ContextWithPrincipal(prepared, fixture.principal)
}

func allowingLimiter() *recordingOperationLimiter {
	return &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}}
}

func (h tunneledSetupHarness) invoke(t *testing.T, ctx context.Context, input GetTunneledMCPSetupHandoffInput) (GetTunneledMCPSetupHandoffOutput, error) {
	t.Helper()
	arguments, err := json.Marshal(input)
	require.NoError(t, err)
	out, err := h.tool.Invoke(ctx, arguments)
	if err != nil {
		return GetTunneledMCPSetupHandoffOutput{}, err
	}
	output, ok := out.(GetTunneledMCPSetupHandoffOutput)
	require.True(t, ok)
	return output, nil
}

func requireTunneledSetupRefusal(t *testing.T, err error, code string) {
	t.Helper()
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	var payload featureUnavailableResult
	require.NoError(t, json.Unmarshal([]byte(refusal.Payload), &payload))
	require.Equal(t, code, payload.Code)
	require.Equal(t, tunneledMCPSetupFeature, payload.Feature)
	require.NotContains(t, refusal.Payload, tunnelStatusSentinel)
	require.NotContains(t, refusal.Payload, "https://")
}

// grantSetupAdmin grants organization administration plus read of the project
// and of every MCP server and tunneled source in it.
func (f tunnelStatusFixture) grantSetupAdmin(t *testing.T) {
	t.Helper()
	f.grant(t, authz.ScopeOrgAdmin, f.principal.OrganizationID, false)
	f.grantProjectSourceRead(t)
}

func TestTunneledSetupHandoffReturnsAddFormURL(t *testing.T) {
	t.Parallel()

	fixture := seedTunnelStatusFixture(t, "platform_mcp_tunneled_setup_add")
	fixture.grantSetupAdmin(t)
	harness, ctx := newTunneledSetupHarness(t, fixture, "https://dashboard.example.test/console?ignored=1#ignored", allowingLimiter(), true)

	output, err := harness.invoke(t, ctx, GetTunneledMCPSetupHandoffInput{ProjectID: fixture.project.ID.String()})
	require.NoError(t, err)

	require.Equal(t, GetTunneledMCPSetupHandoffOutput{
		ProjectID:    fixture.project.ID.String(),
		ProjectSlug:  fixture.project.Slug,
		Intent:       TunneledMCPSetupIntentAdd,
		SetupURL:     "https://dashboard.example.test/console/" + harness.orgSlug + "/projects/" + fixture.project.Slug + "/mcp/add/tunneled",
		Instructions: tunneledMCPAddInstructions,
	}, output)
	require.Len(t, harness.limiter.keys, 2, "one handoff charges the actor and the organization once each")
}

func TestTunneledSetupHandoffReturnsAgentSetupURLForExistingServer(t *testing.T) {
	t.Parallel()

	fixture := seedTunnelStatusFixture(t, "platform_mcp_tunneled_setup_existing")
	fixture.grantSetupAdmin(t)
	harness, ctx := newTunneledSetupHarness(t, fixture, "https://dashboard.example.test", allowingLimiter(), true)

	output, err := harness.invoke(t, ctx, GetTunneledMCPSetupHandoffInput{ProjectID: fixture.project.ID.String(), MCPID: fixture.wrapperID.String()})
	require.NoError(t, err)

	require.Equal(t, fixture.wrapperID.String(), output.MCPID)
	require.Equal(t, TunneledMCPSetupIntentAgent, output.Intent)
	require.Equal(t, "https://dashboard.example.test/"+harness.orgSlug+"/projects/"+fixture.project.Slug+"/mcp/x/private-inventory/settings#agent-setup", output.SetupURL)
	require.Equal(t, tunneledMCPAgentInstructions, output.Instructions)

	encoded, err := json.Marshal(output)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), tunnelStatusSentinel, "no key, key prefix, or resource identifier may reach the handoff")
}

func TestTunneledSetupHandoffRefusesNonAdministrator(t *testing.T) {
	t.Parallel()

	fixture := seedTunnelStatusFixture(t, "platform_mcp_tunneled_setup_non_admin")
	fixture.grantProjectSourceRead(t)
	harness, ctx := newTunneledSetupHarness(t, fixture, "https://dashboard.example.test", allowingLimiter(), true)

	for _, input := range []GetTunneledMCPSetupHandoffInput{
		{ProjectID: fixture.project.ID.String()},
		{ProjectID: fixture.project.ID.String(), MCPID: fixture.wrapperID.String()},
	} {
		_, err := harness.invoke(t, ctx, input)
		requireTunneledSetupRefusal(t, err, "permission_denied")
	}
	require.Empty(t, harness.limiter.keys, "a refused caller must not spend the organization's handoff budget")
}

func TestTunneledSetupHandoffHidesProjectWithoutProjectRead(t *testing.T) {
	t.Parallel()

	fixture := seedTunnelStatusFixture(t, "platform_mcp_tunneled_setup_admin_only")
	fixture.grant(t, authz.ScopeOrgAdmin, fixture.principal.OrganizationID, false)
	harness, ctx := newTunneledSetupHarness(t, fixture, "https://dashboard.example.test", allowingLimiter(), true)

	_, err := harness.invoke(t, ctx, GetTunneledMCPSetupHandoffInput{ProjectID: fixture.project.ID.String()})
	requireTunneledSetupRefusal(t, err, "not_found")
	require.Empty(t, harness.limiter.keys)
}

func TestTunneledSetupHandoffHidesServerWithoutProjectSourceRead(t *testing.T) {
	t.Parallel()

	fixture := seedTunnelStatusFixture(t, "platform_mcp_tunneled_setup_wrapper_only")
	fixture.grant(t, authz.ScopeOrgAdmin, fixture.principal.OrganizationID, false)
	fixture.grant(t, authz.ScopeProjectRead, fixture.project.ID.String(), false)
	fixture.grant(t, authz.ScopeMCPRead, fixture.wrapperID.String(), true)
	harness, ctx := newTunneledSetupHarness(t, fixture, "https://dashboard.example.test", allowingLimiter(), true)

	_, err := harness.invoke(t, ctx, GetTunneledMCPSetupHandoffInput{ProjectID: fixture.project.ID.String(), MCPID: fixture.wrapperID.String()})
	requireTunneledSetupRefusal(t, err, "not_found")
	require.Empty(t, harness.limiter.keys)
}

func TestTunneledSetupHandoffRefusesNonTunneledServer(t *testing.T) {
	t.Parallel()

	fixture := seedTunnelStatusFixture(t, "platform_mcp_tunneled_setup_not_tunneled")
	fixture.grantSetupAdmin(t)
	harness, ctx := newTunneledSetupHarness(t, fixture, "https://dashboard.example.test", allowingLimiter(), true)

	_, err := harness.invoke(t, ctx, GetTunneledMCPSetupHandoffInput{ProjectID: fixture.project.ID.String(), MCPID: fixture.otherMCP.String()})
	requireTunneledSetupRefusal(t, err, "not_tunneled")
}

func TestTunneledSetupHandoffHidesDeletedSource(t *testing.T) {
	t.Parallel()

	fixture := seedTunnelStatusFixture(t, "platform_mcp_tunneled_setup_deleted_source")
	fixture.grantSetupAdmin(t)
	_, err := tunneledmcprepo.New(fixture.conn).DeleteServer(t.Context(), tunneledmcprepo.DeleteServerParams{ID: fixture.tunnelID, ProjectID: fixture.project.ID})
	require.NoError(t, err)
	harness, ctx := newTunneledSetupHarness(t, fixture, "https://dashboard.example.test", allowingLimiter(), true)

	_, err = harness.invoke(t, ctx, GetTunneledMCPSetupHandoffInput{ProjectID: fixture.project.ID.String(), MCPID: fixture.wrapperID.String()})
	requireTunneledSetupRefusal(t, err, "not_found")
}

func TestTunneledSetupHandoffHidesMissingAndForeignTargets(t *testing.T) {
	t.Parallel()

	fixture := seedTunnelStatusFixture(t, "platform_mcp_tunneled_setup_missing")
	fixture.grantSetupAdmin(t)
	foreign := seedTunnelStatusFixture(t, "platform_mcp_tunneled_setup_foreign")
	harness, ctx := newTunneledSetupHarness(t, fixture, "https://dashboard.example.test", allowingLimiter(), true)

	for _, input := range []GetTunneledMCPSetupHandoffInput{
		{ProjectID: uuid.NewString()},
		{ProjectID: fixture.project.ID.String(), MCPID: uuid.NewString()},
		{ProjectID: fixture.project.ID.String(), MCPID: foreign.wrapperID.String()},
	} {
		_, err := harness.invoke(t, ctx, input)
		requireTunneledSetupRefusal(t, err, "not_found")
	}
}

func TestTunneledSetupHandoffRejectsMalformedTarget(t *testing.T) {
	t.Parallel()

	fixture := seedTunnelStatusFixture(t, "platform_mcp_tunneled_setup_malformed")
	fixture.grantSetupAdmin(t)
	harness, ctx := newTunneledSetupHarness(t, fixture, "https://dashboard.example.test", allowingLimiter(), true)

	_, err := harness.invoke(t, ctx, GetTunneledMCPSetupHandoffInput{ProjectID: fixture.project.ID.String(), MCPID: "not-a-uuid"})
	requireTunneledSetupRefusal(t, err, "invalid_request")
	require.Empty(t, harness.limiter.keys)
}

func TestTunneledSetupHandoffReportsExhaustedBudget(t *testing.T) {
	t.Parallel()

	fixture := seedTunnelStatusFixture(t, "platform_mcp_tunneled_setup_budget")
	fixture.grantSetupAdmin(t)
	harness, ctx := newTunneledSetupHarness(t, fixture, "https://dashboard.example.test", &recordingOperationLimiter{result: ratelimit.Result{Allowed: false}}, true)

	_, err := harness.invoke(t, ctx, GetTunneledMCPSetupHandoffInput{ProjectID: fixture.project.ID.String()})
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Contains(t, refusal.Payload, `"code":"rate_limited"`)
	require.NotContains(t, refusal.Payload, "https://")
}

func TestTunneledSetupHandoffRefusesAddFormWhenTunnelsAreNotEnabled(t *testing.T) {
	t.Parallel()

	fixture := seedTunnelStatusFixture(t, "platform_mcp_tunneled_setup_flag_disabled")
	fixture.grantSetupAdmin(t)
	harness, ctx := newTunneledSetupHarness(t, fixture, "https://dashboard.example.test", allowingLimiter(), true)
	harness.flags.SetFlag(feature.FlagTunneledMCP, fixture.principal.OrganizationID, false)

	_, err := harness.invoke(t, ctx, GetTunneledMCPSetupHandoffInput{ProjectID: fixture.project.ID.String()})
	requireTunneledSetupRefusal(t, err, "not_enabled")
	require.Len(t, harness.limiter.keys, 2, "an authorized request is metered even when the add form is not available")

	// An existing tunneled server's settings page is not behind the rollout.
	output, err := harness.invoke(t, ctx, GetTunneledMCPSetupHandoffInput{ProjectID: fixture.project.ID.String(), MCPID: fixture.wrapperID.String()})
	require.NoError(t, err)
	require.Equal(t, TunneledMCPSetupIntentAgent, output.Intent)
}

func TestTunneledSetupHandoffReportsAddFormUnavailableWhenRolloutIsIndeterminate(t *testing.T) {
	t.Parallel()

	fixture := seedTunnelStatusFixture(t, "platform_mcp_tunneled_setup_flag_unknown")
	fixture.grantSetupAdmin(t)
	// A flag the provider holds no decision for is indeterminate, not off.
	harness, ctx := newTunneledSetupHarness(t, fixture, "https://dashboard.example.test", allowingLimiter(), false)

	_, err := harness.invoke(t, ctx, GetTunneledMCPSetupHandoffInput{ProjectID: fixture.project.ID.String()})
	requireTunneledSetupRefusal(t, err, unavailableCode)
	require.Len(t, harness.limiter.keys, 2)
}
