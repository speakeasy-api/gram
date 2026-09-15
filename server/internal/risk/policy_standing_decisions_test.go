package risk_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/advisories"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/domainmeta"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/evidence"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/packagemeta"
	mcpapprovalrepo "github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repometa"
	"github.com/speakeasy-api/gram/server/internal/oops"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// newTestRiskServiceWithRealIntake wires the real mcpapproval service as the
// approval intake, the way the server wires it — the backfill under test is
// the intake's, and a fake would prove nothing.
func newTestRiskServiceWithRealIntake(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	flags := &feature.InMemory{}
	ctx, instance := newTestRiskService(t, func(instance *testInstance) {
		logger := testenv.NewLogger(t)
		tracerProvider := testenv.NewTracerProvider(t)

		authzEngine := authz.NewEngine(logger, instance.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
		assembler := evidence.NewAssembler(
			packagemeta.NewClient(riskIntakeNotFoundRegistry{}),
			repometa.NewClient(riskIntakeNotFoundRegistry{}),
			advisories.NewClient(riskIntakeEmptyAdvisoryDB{}),
			domainmeta.NewClient(riskIntakeNotFoundRegistry{}),
			telemetryrepo.New(instance.chConn),
			riskIntakeQuietProbes{},
			riskIntakeQuietProbes{},
			riskIntakeQuietProbes{},
		)

		instance.approvalIntake = mcpapproval.NewService(logger, tracerProvider, instance.conn, instance.sessionManager, authzEngine, flags, audit.NewLogger(), assembler, nil)
	})

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	flags.SetFlag(feature.FlagMCPApproval, authCtx.ActiveOrganizationID, true)

	return ctx, instance
}

// seedStandingDecision plants a decided review the way history would have
// left it: a server_url request whose latest decision carries the recorded
// blast radius. Written as fixture rows because the scenario under test is
// precisely "the decision predates the policy" — at decision time there was
// no blocking policy to write grants on.
func seedStandingDecision(t *testing.T, ctx context.Context, ti *testInstance, serverURL, decision string, grantedPrincipalURNs []string) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	queries := mcpapprovalrepo.New(ti.conn)
	request, err := queries.UpsertApprovalRequest(ctx, mcpapprovalrepo.UpsertApprovalRequestParams{
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		TargetKind:                "server_url",
		TargetRaw:                 serverURL,
		TargetKey:                 serverURL,
		ArtifactRef:               pgtype.Text{String: "", Valid: false},
		VersionPinned:             false,
		Status:                    decision,
		RiskPolicyBypassRequestID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)

	_, err = queries.CreateApprovalDecision(ctx, mcpapprovalrepo.CreateApprovalDecisionParams{
		OrganizationID:       authCtx.ActiveOrganizationID,
		ProjectID:            *authCtx.ProjectID,
		McpApprovalRequestID: request.ID,
		Decision:             decision,
		DecidedBy:            "test-admin",
		Rationale:            pgtype.Text{String: "", Valid: false},
		EvidenceSnapshot:     []byte("{}"),
		EvidenceVersion:      1,
		GrantedPrincipalUrns: grantedPrincipalURNs,
		McpResearchReportID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
}

// A blocking policy created after decisions were recorded honors them:
// the standing approval gets its bypass audience on the new policy, and the
// standing denial gets nothing — blocked is the policy's default. Without
// the backfill, both would be blocked while one still read approved.
func TestCreateRiskPolicy_HonorsStandingDecisions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskServiceWithRealIntake(t)

	approvedURL := "https://mcp.example.com/approved-before-policy"
	deniedURL := "https://mcp.example.com/denied-before-policy"
	seedStandingDecision(t, ctx, ti, approvedURL, "approved", []string{authz.AllUsersPrincipal().String()})
	seedStandingDecision(t, ctx, ti, deniedURL, "denied", []string{})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Sources: []string{"shadow_mcp"},
		Action:  "block",
	})
	require.NoError(t, err)

	principals := shadowMCPPolicyURLPrincipals(t, ctx, ti.conn, policy.ID)
	require.Equal(t, []string{authz.AllUsersPrincipal().String()}, principals[approvedURL],
		"the standing approval's blast radius lands on the new policy")
	require.NotContains(t, principals, deniedURL,
		"a standing denial writes nothing — blocked is the default")
}

// A narrower-than-everyone approval carries its recorded principals onto the
// new block-by-default policy, so the enforcement matches the decision's
// blast radius rather than widening it.
func TestCreateRiskPolicy_HonorsNarrowStandingApproval(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskServiceWithRealIntake(t)

	narrowURL := "https://mcp.example.com/narrow-approval"
	person := urn.NewPrincipal(urn.PrincipalTypeUser, "user_backfill_test").String()
	seedStandingDecision(t, ctx, ti, narrowURL, "approved", []string{person})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Sources: []string{"shadow_mcp"},
		Action:  "block",
	})
	require.NoError(t, err)

	principals := shadowMCPPolicyURLPrincipals(t, ctx, ti.conn, policy.ID)
	require.Equal(t, []string{person}, principals[narrowURL])
}

// An allow-by-default policy inverts the directions, exactly as decision-time
// enforcement does: the standing denial writes a block rule for everyone,
// and the standing approval writes nothing — allowed is the default.
func TestCreateRiskPolicy_AllowAllHonorsStandingDenials(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskServiceWithRealIntake(t)

	approvedURL := "https://mcp.example.com/allowed-anyway"
	deniedURL := "https://mcp.example.com/denied-under-allow-all"
	seedStandingDecision(t, ctx, ti, approvedURL, "approved", []string{authz.AllUsersPrincipal().String()})
	seedStandingDecision(t, ctx, ti, deniedURL, "denied", []string{})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpDisposition: new("allow_all"),
	})
	require.NoError(t, err)

	require.Equal(t, []string{deniedURL}, shadowMCPPolicyBlockedURLs(t, ctx, ti.conn, policy.ID))
}

// An allow_all policy cannot express an approval scoped to specific people —
// there is no per-principal allow under allow-by-default — so creating one
// against a standing narrow approval fails with the servers named, instead
// of silently widening what the decision recorded.
func TestCreateRiskPolicy_AllowAllRejectsNarrowStandingApprovals(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskServiceWithRealIntake(t)

	narrowURL := "https://mcp.example.com/narrow-blocks-allow-all"
	person := urn.NewPrincipal(urn.PrincipalTypeUser, "user_narrow_reject").String()
	seedStandingDecision(t, ctx, ti, narrowURL, "approved", []string{person})

	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpDisposition: new("allow_all"),
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.Contains(t, err.Error(), narrowURL, "the rejection names the offending server")
}

// Decisions recorded before RecordDecision normalized an empty approved set
// stored ARRAY[] for an everyone-approval. Replaying that literally would
// grant nobody — an approved server still blocked, the exact contradiction
// the backfill removes — so the replay applies the same normalization the
// writer does today.
func TestCreateRiskPolicy_EmptyApprovedPrincipalsMeanEveryone(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskServiceWithRealIntake(t)

	legacyURL := "https://mcp.example.com/legacy-empty-approval"
	seedStandingDecision(t, ctx, ti, legacyURL, "approved", []string{})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Sources: []string{"shadow_mcp"},
		Action:  "block",
	})
	require.NoError(t, err)

	principals := shadowMCPPolicyURLPrincipals(t, ctx, ti.conn, policy.ID)
	require.Equal(t, []string{authz.AllUsersPrincipal().String()}, principals[legacyURL],
		"an empty approved set is an everyone-approval, not a grant to nobody")
}

// The same legacy row must not read as person-scoped either: an allow_all
// policy creation proceeds, because an everyone-approval is expressible
// there (it simply writes nothing).
func TestCreateRiskPolicy_AllowAllAcceptsEmptyApprovedPrincipals(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskServiceWithRealIntake(t)

	seedStandingDecision(t, ctx, ti, "https://mcp.example.com/legacy-empty-allow-all", "approved", []string{})

	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpDisposition: new("allow_all"),
	})
	require.NoError(t, err)
}

// Enabling a disabled blocking policy is the same moment as creating one: it
// starts enforcing, so the standing decisions apply to it right then.
func TestUpdateRiskPolicy_TransitionIntoBlockingBackfills(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskServiceWithRealIntake(t)

	approvedURL := "https://mcp.example.com/approved-before-enable"
	seedStandingDecision(t, ctx, ti, approvedURL, "approved", []string{authz.AllUsersPrincipal().String()})

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Sources: []string{"shadow_mcp"},
		Action:  "block",
		Enabled: new(false),
	})
	require.NoError(t, err)
	require.Empty(t, shadowMCPPolicyURLPrincipals(t, ctx, ti.conn, created.ID),
		"a disabled policy enforces nothing, so nothing is derived for it")

	updated, err := ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:      created.ID,
		Name:    created.Name,
		Enabled: new(true),
	})
	require.NoError(t, err)

	principals := shadowMCPPolicyURLPrincipals(t, ctx, ti.conn, updated.ID)
	require.Equal(t, []string{authz.AllUsersPrincipal().String()}, principals[approvedURL],
		"the transition into enforcing derives the standing approval's grant")
}
