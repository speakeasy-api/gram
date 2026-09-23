package risk_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type fixedMCPPolicyLookup []policycore.Policy

func (p fixedMCPPolicyLookup) ListEnabledForMCPServer(context.Context, string, uuid.UUID, uuid.UUID, string) ([]policycore.Policy, error) {
	return p, nil
}

func TestMCPPolicyScanner_RemoteShadowMCPIsSilent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := *authCtx.ProjectID
	serverID := uuid.New()
	policy := policycore.Policy{
		ID:             uuid.New(),
		ProjectID:      projectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           "Shadow MCP",
		Sources:        []string{shadowmcp.SourceShadowMCP},
		Action:         "block",
	}
	evaluator := mcpriskscan.NewPolicyEvaluator(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		fixedMCPPolicyLookup{policy},
		risk.NewMCPPolicyScanner(newShadowMCPTestScanner(t, ti), nil),
		gcp.NewNoopPublisher[*riskv1.Finding](),
		mcpriskscan.DefaultPolicyConfig,
	)

	decision := evaluator.Scan(ctx, mcpriskscan.NewRequest(ctx, mcpriskscan.Event{
		Surface:        mcpriskscan.SurfaceRemoteMCP,
		Method:         mcpriskscan.MethodToolsCall,
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID.String(),
		ServerID:       serverID.String(),
		MetaServerID:   "",
		ToolsetID:      "",
		ToolName:       "lookup",
		ResourceURI:    "",
		PromptName:     "",
		ChatID:         "",
	}, mcpriskscan.BorrowPayload([]byte(`{"query":"sample"}`))))

	require.False(t, decision.Denied())
	require.False(t, decision.Indeterminate)
}

func TestMCPPolicyScanner_AccountIdentityDoesNotMakeGitleaksIndeterminate(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	detector := risk.NewMCPPolicyScanner(newShadowMCPTestScanner(t, ti), nil)

	findings, err := detector.ScanMCPPolicy(ctx, policycore.Policy{
		ID:             uuid.New(),
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		Sources:        []string{risk_analysis.SourceAccountIdentity, risk_analysis.SourceGitleaks},
	}, risk.MCPScanRequest{
		Text:      `{"token":"ASIAZ2XY3WNBQR5TUVWX"}`,
		ToolName:  "lookup",
		ToolsetID: "",
		ServerID:  uuid.NewString(),
		UserID:    authCtx.UserID,
	})

	require.NoError(t, err)
	require.NotEmpty(t, findings)
	for _, finding := range findings {
		require.Equal(t, risk_analysis.SourceGitleaks, finding.Source)
	}
}
