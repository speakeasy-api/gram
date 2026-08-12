package risk_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestCreateRiskPolicy_SecondEnabledShadowMCPBlockingPolicyRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)

	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("First Shadow MCP"),
		Sources: []string{"shadow_mcp"},
		Action:  "block",
	})
	require.NoError(t, err)

	name := "Second Shadow MCP"
	_, err = ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 &name,
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpDisposition: new("allow_all"),
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeConflict, oopsErr.Code)
	require.False(t, riskPolicyExistsByName(t, ctx, ti.conn, name))
}

func TestCreateRiskPolicy_SecondDisabledShadowMCPBlockingPolicyAllowed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)

	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Enabled Shadow MCP"),
		Sources: []string{"shadow_mcp"},
		Action:  "block",
	})
	require.NoError(t, err)

	disabled := false
	_, err = ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Disabled Shadow MCP"),
		Sources: []string{"shadow_mcp"},
		Action:  "block",
		Enabled: &disabled,
	})
	require.NoError(t, err)
}

func TestCreateRiskPolicy_ShadowMCPFlagPolicyDoesNotCountTowardLimit(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)

	// A flag-action shadow_mcp policy observes without blocking, so it does
	// not conflict with a blocking policy.
	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Observing Shadow MCP"),
		Sources: []string{"shadow_mcp"},
		Action:  "flag",
	})
	require.NoError(t, err)

	_, err = ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Blocking Shadow MCP"),
		Sources: []string{"shadow_mcp"},
		Action:  "block",
	})
	require.NoError(t, err)
}

func TestUpdateRiskPolicy_EnablingSecondShadowMCPBlockingPolicyRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)

	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Live Shadow MCP"),
		Sources: []string{"shadow_mcp"},
		Action:  "block",
	})
	require.NoError(t, err)

	disabled := false
	second, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Standby Shadow MCP"),
		Sources: []string{"shadow_mcp"},
		Action:  "block",
		Enabled: &disabled,
	})
	require.NoError(t, err)

	enabled := true
	_, err = ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:      second.ID,
		Name:    second.Name,
		Enabled: &enabled,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeConflict, oopsErr.Code)
}

func TestUpdateRiskPolicy_SoleShadowMCPBlockingPolicyUpdatable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Sole Shadow MCP"),
		Sources: []string{"shadow_mcp"},
		Action:  "block",
	})
	require.NoError(t, err)

	updated, err := ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:   created.ID,
		Name: "Renamed Sole Shadow MCP",
	})
	require.NoError(t, err)
	require.Equal(t, "Renamed Sole Shadow MCP", updated.Name)
}

func TestScanner_LookupShadowMCPBlockingPolicy_CarriesDispositionAndBlocklist(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 new("Allow All Lookup"),
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpDisposition: new("allow_all"),
		ShadowMcpBlockedUrls: []string{"https://sketchy.example.com/mcp"},
	})
	require.NoError(t, err)

	scanner, err := risk.NewScanner(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		ti.conn,
		newTestCustomRuleAnalyzer(t, ti.conn),
		nil,
		nil,
		nil,
		nil,
		testCELEngine(t),
	)
	require.NoError(t, err)

	policy, err := scanner.LookupShadowMCPBlockingPolicy(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID)
	require.NoError(t, err)
	require.NotNil(t, policy)
	require.Equal(t, created.ID, policy.ID)
	require.Equal(t, "allow_all", policy.Disposition)
	require.Equal(t, []string{"https://sketchy.example.com/mcp"}, policy.BlockedURLs)
}

func TestScanner_LookupShadowMCPBlockingPolicy_BlockAllDisposition(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)

	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Block All Lookup"),
		Sources: []string{"shadow_mcp"},
		Action:  "block",
	})
	require.NoError(t, err)

	scanner, err := risk.NewScanner(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		ti.conn,
		newTestCustomRuleAnalyzer(t, ti.conn),
		nil,
		nil,
		nil,
		nil,
		testCELEngine(t),
	)
	require.NoError(t, err)

	policy, err := scanner.LookupShadowMCPBlockingPolicy(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID)
	require.NoError(t, err)
	require.NotNil(t, policy)
	require.Equal(t, "block_all", policy.Disposition)
	require.Empty(t, policy.BlockedURLs)
}
