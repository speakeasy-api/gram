package risk_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/scanners/shadowmcpscan"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type offlineBypassValidator struct{}

func (offlineBypassValidator) ValidateToolsetCall(context.Context, any, string, string) (string, bool) {
	return "", true
}

type offlineBypassHostedChecker struct{}

func (offlineBypassHostedChecker) TrustedMCPHostsForOrg(context.Context, string) ([]string, error) {
	return nil, nil
}

type offlineBypassProvenance struct {
	found map[string]telemetryrepo.MCPProvenance
}

func (p offlineBypassProvenance) LookupMCPProvenanceByToolCallID(context.Context, uuid.UUID, []string, time.Time) (map[string]telemetryrepo.MCPProvenance, error) {
	return p.found, nil
}

type offlineBypassChecker struct {
	evaluator *risk.PolicyBypassEvaluator
	calls     int
}

func (c *offlineBypassChecker) CanBypassShadowMCP(
	ctx context.Context,
	organizationID string,
	policyID uuid.UUID,
	requests []shadowmcpscan.BypassRequest,
) map[shadowmcpscan.BypassRequest]bool {
	c.calls++
	evaluationRequests := make(map[risk.PolicyBypassEvaluation]shadowmcpscan.BypassRequest, len(requests))
	evaluations := make([]risk.PolicyBypassEvaluation, 0, len(requests))
	for _, request := range requests {
		target := risk.ShadowMCPPolicyBypassTarget(request.Evidence, request.ToolName)
		if target == nil {
			continue
		}
		evaluation := risk.PolicyBypassEvaluation{
			OrganizationID: organizationID,
			UserID:         request.UserID,
			PolicyID:       policyID.String(),
			Target:         target,
		}
		evaluationRequests[evaluation] = request
		evaluations = append(evaluations, evaluation)
	}
	results := make(map[shadowmcpscan.BypassRequest]bool, len(requests))
	for evaluation, allowed := range c.evaluator.CanBypassBatch(ctx, evaluations) {
		results[evaluationRequests[evaluation]] = allowed
	}
	return results
}

type countingBypassDB struct {
	accessrepo.DBTX
	readCalls int
}

func (d *countingBypassDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	d.readCalls++
	rows, err := d.DBTX.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("run counted bypass query: %w", err)
	}
	return rows, nil
}

func (d *countingBypassDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	d.readCalls++
	return d.DBTX.QueryRow(ctx, sql, args...)
}

func TestOfflineShadowMCPScan_CursorApprovalWithURLAndIdentitySuppressesFinding(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	require.NotNil(t, authCtx.ProjectID)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID))

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new("Offline Shadow MCP Cursor Bypass"),
	})
	require.NoError(t, err)

	serverURL := "https://mcp.example.test/sse"
	serverIdentity := "mcp.example.test"
	token, _, err := risk.GeneratePolicyBypassRequestToken(ctx, ti.cacheAdapter, risk.PolicyBypassRequestTokenInput{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              authCtx.ProjectID.String(),
		RequesterUserID:        authCtx.UserID,
		ObservedName:           nil,
		ObservedFullURL:        &serverURL,
		ObservedURLHost:        nil,
		ObservedServerIdentity: &serverIdentity,
		ToolName:               new("MCP:authenticate"),
		ToolCall:               nil,
		BlockReason:            new("Blocked by Shadow MCP policy"),
		RiskPolicyID:           policy.ID,
		RiskResultID:           nil,
	}, 5*time.Minute)
	require.NoError(t, err)

	request, err := ti.service.CreateRiskPolicyBypassRequest(ctx, &gen.CreateRiskPolicyBypassRequestPayload{
		RequestToken: token,
	})
	require.NoError(t, err)
	approved, err := ti.service.ApproveRiskPolicyBypassRequest(ctx, &gen.ApproveRiskPolicyBypassRequestPayload{
		ID: request.ID,
	})
	require.NoError(t, err)
	require.Equal(t, "approved", approved.Status)
	require.Equal(t, serverURL, approved.TargetDimensions[authz.SelectorKeyServerURL])
	require.Equal(t, serverIdentity, approved.TargetDimensions[authz.SelectorKeyServerIdentity])

	countingDB := &countingBypassDB{DBTX: ti.conn, readCalls: 0}
	checker := &offlineBypassChecker{
		evaluator: risk.NewPolicyBypassEvaluator(testenv.NewLogger(t), countingDB),
		calls:     0,
	}
	scanner := shadowmcpscan.NewScannerWithBypass(
		testenv.NewLogger(t),
		offlineBypassValidator{},
		offlineBypassHostedChecker{},
		offlineBypassProvenance{found: map[string]telemetryrepo.MCPProvenance{
			"cursor-call-1": {Match: serverURL, ServerURL: serverURL, ServerIdentity: serverIdentity, HookSource: "cursor"},
			"cursor-call-2": {Match: serverURL, ServerURL: serverURL, ServerIdentity: serverIdentity, HookSource: "cursor"},
		}},
		nil,
		checker,
	)

	findings := scanner.Scan(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, uuid.MustParse(policy.ID), []shadowmcpscan.Message{{
		UserID: authCtx.UserID,
		ToolCalls: []shadowmcpscan.ToolCall{
			{ID: "cursor-call-1", Name: "MCP:authenticate", Arguments: `{}`, CreatedAt: time.Now(), Sender: "Cursor"},
			{ID: "cursor-call-2", Name: "MCP:list_events", Arguments: `{}`, CreatedAt: time.Now(), Sender: "Cursor"},
		},
	}})

	require.Equal(t, 1, checker.calls, "the scan evaluates all finding candidates in one batch")
	require.Equal(t, 3, countingDB.readCalls, "principal membership, roles, and grants load once for one user")
	require.Empty(t, findings[0], "the approved Cursor-style server bypass must suppress offline findings")
}
