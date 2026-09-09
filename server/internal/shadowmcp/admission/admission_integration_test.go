package admission

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	approvalrepo "github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestCheckUsesBlockingPolicyAndLatestStandingDecision(t *testing.T) {
	t.Parallel()

	fixture := newAdmissionFixture(t)
	ctx := t.Context()
	target := "https://mcp.example.test/server"
	desired := []string{"role:developers"}

	tx := testenv.BeginTx(t, ctx, fixture.conn)
	require.NoError(t, LockProject(ctx, tx, fixture.projectID))
	_, err := Check(ctx, tx, "another-organization", fixture.projectID, target, nil)
	require.Error(t, err)
	verdict, err := Check(ctx, tx, fixture.orgID, fixture.projectID, target, desired)
	require.NoError(t, err)
	require.Equal(t, StateNotRequired, verdict.State)
	require.NoError(t, tx.Commit(ctx))

	seedBlockingPolicy(t, fixture)
	requestID := seedDecision(t, fixture, target, "approved", desired)

	tx = testenv.BeginTx(t, ctx, fixture.conn)
	require.NoError(t, LockProject(ctx, tx, fixture.projectID))
	verdict, err = Check(ctx, tx, fixture.orgID, fixture.projectID, target, desired)
	require.NoError(t, err)
	require.Equal(t, StateCovered, verdict.State)
	require.NoError(t, tx.Commit(ctx))

	_, err = approvalrepo.New(fixture.conn).CreateApprovalDecision(ctx, approvalrepo.CreateApprovalDecisionParams{
		OrganizationID:       fixture.orgID,
		ProjectID:            fixture.projectID,
		McpApprovalRequestID: requestID,
		Decision:             "denied",
		DecidedBy:            "test-reviewer",
		Rationale:            conv.ToPGText("new evidence changed the decision"),
		EvidenceSnapshot:     []byte(`{}`),
		EvidenceVersion:      1,
		GrantedPrincipalUrns: []string{},
		McpResearchReportID:  uuid.NullUUID{},
	})
	require.NoError(t, err)

	tx = testenv.BeginTx(t, ctx, fixture.conn)
	require.NoError(t, LockProject(ctx, tx, fixture.projectID))
	verdict, err = Check(ctx, tx, fixture.orgID, fixture.projectID, target, desired)
	require.NoError(t, err)
	require.Equal(t, StateApprovalRequired, verdict.State)
	require.NoError(t, tx.Commit(ctx))
}

func TestCheckRejectsSupersededAndCrossOrganizationDecisions(t *testing.T) {
	t.Parallel()

	fixture := newAdmissionFixture(t)
	ctx := t.Context()
	target := "https://mcp.example.test/superseded"
	seedBlockingPolicy(t, fixture)
	requestID := seedDecision(t, fixture, target, "approved", []string{"role:developers"})
	require.NoError(t, approvalrepo.New(fixture.conn).SetApprovalRequestStatus(ctx, approvalrepo.SetApprovalRequestStatusParams{
		Status:    "superseded",
		ID:        requestID,
		ProjectID: fixture.projectID,
	}))

	tx := testenv.BeginTx(t, ctx, fixture.conn)
	require.NoError(t, LockProject(ctx, tx, fixture.projectID))
	verdict, err := Check(ctx, tx, fixture.orgID, fixture.projectID, target, []string{"role:developers"})
	require.NoError(t, err)
	require.Equal(t, StateApprovalRequired, verdict.State)

	_, err = Check(ctx, tx, "another-organization", fixture.projectID, target, []string{"role:developers"})
	require.Error(t, err)
	require.NoError(t, tx.Commit(ctx))
}

func TestLockProjectSerializesTransactions(t *testing.T) {
	t.Parallel()

	fixture := newAdmissionFixture(t)
	ctx := t.Context()
	first := testenv.BeginTx(t, ctx, fixture.conn)
	require.NoError(t, LockProject(ctx, first, fixture.projectID))

	second := testenv.BeginTx(t, ctx, fixture.conn)
	acquired := make(chan error, 1)
	go func() {
		acquired <- LockProject(ctx, second, fixture.projectID)
	}()

	require.Never(t, func() bool {
		return len(acquired) > 0
	}, 100*time.Millisecond, 10*time.Millisecond)

	require.NoError(t, first.Rollback(ctx))
	require.Eventually(t, func() bool {
		return len(acquired) > 0
	}, time.Second, 10*time.Millisecond)
	require.NoError(t, <-acquired)
}

func seedBlockingPolicy(t *testing.T, fixture admissionFixture) {
	t.Helper()

	_, err := riskrepo.New(fixture.conn).CreateRiskPolicy(t.Context(), riskrepo.CreateRiskPolicyParams{
		ID:                   uuid.New(),
		ProjectID:            fixture.projectID,
		OrganizationID:       fixture.orgID,
		Name:                 "Blocking Shadow MCP",
		PolicyType:           "standard",
		Sources:              []string{shadowmcp.SourceShadowMCP},
		PresidioEntities:     nil,
		AnalyzerConfig:       nil,
		PromptInjectionRules: nil,
		DisabledRules:        nil,
		CustomRuleIds:        nil,
		MessageTypes:         nil,
		ScopeInclude:         pgtype.Text{},
		ScopeExempt:          pgtype.Text{},
		Enabled:              true,
		Action:               "block",
		AudienceType:         "everyone",
		ShadowMcpDisposition: pgtype.Text{},
		AutoName:             false,
		UserMessage:          pgtype.Text{},
		Prompt:               pgtype.Text{},
		ModelConfig:          nil,
		Score:                pgtype.Float8{},
	})
	require.NoError(t, err)
}

func seedDecision(t *testing.T, fixture admissionFixture, target, decision string, principals []string) uuid.UUID {
	t.Helper()

	request, err := approvalrepo.New(fixture.conn).UpsertApprovalRequest(t.Context(), approvalrepo.UpsertApprovalRequestParams{
		OrganizationID:            fixture.orgID,
		ProjectID:                 fixture.projectID,
		TargetKind:                "server_url",
		TargetRaw:                 target,
		TargetKey:                 target,
		ArtifactRef:               pgtype.Text{},
		VersionPinned:             false,
		Status:                    "requested",
		RiskPolicyBypassRequestID: uuid.NullUUID{},
	})
	require.NoError(t, err)
	_, err = approvalrepo.New(fixture.conn).CreateApprovalDecision(t.Context(), approvalrepo.CreateApprovalDecisionParams{
		OrganizationID:       fixture.orgID,
		ProjectID:            fixture.projectID,
		McpApprovalRequestID: request.ID,
		Decision:             decision,
		DecidedBy:            "test-reviewer",
		Rationale:            conv.ToPGText("test decision"),
		EvidenceSnapshot:     []byte(`{}`),
		EvidenceVersion:      1,
		GrantedPrincipalUrns: principals,
		McpResearchReportID:  uuid.NullUUID{},
	})
	require.NoError(t, err)
	return request.ID
}
