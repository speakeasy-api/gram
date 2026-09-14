package platformmcp

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
	approvalrepo "github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestShadowDecisionDeniesAndReplaysAtomically(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_shadow_decision")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	request, err := approvalrepo.New(conn).UpsertApprovalRequest(ctx, approvalrepo.UpsertApprovalRequestParams{
		OrganizationID: principal.OrganizationID, ProjectID: project.ID,
		TargetKind: "server_url", TargetRaw: "https://shadow.example.test/mcp", TargetKey: "https://shadow.example.test/mcp",
		ArtifactRef: pgtype.Text{}, VersionPinned: false, Status: "requested", RiskPolicyBypassRequestID: uuid.NullUUID{},
	})
	require.NoError(t, err)
	require.NoError(t, approvalrepo.New(conn).SetApprovalRequestEvidence(ctx, approvalrepo.SetApprovalRequestEvidenceParams{CurrentEvidence: []byte(`{"identity":{"kind":"remote"}}`), EvidenceVersion: 1, ID: request.ID, ProjectID: project.ID}))

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagMCPApproval, principal.OrganizationID, true)
	flags.SetFlag(feature.FlagPlatformMCPShadowAccessDecisions, principal.OrganizationID, true)
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	redisClient, err := platformMCPInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	sessions := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("shadow-decision"), billing.NewStubClient(logger, tracerProvider))
	core := mcpapproval.NewService(logger, tracerProvider, conn, sessions, authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, nil), flags, audit.NewLogger(), nil, nil)
	references, err := newSubjectReferenceCodec("shadow-decision-key")
	require.NoError(t, err)
	versions, err := newShadowDecisionVersionCodec("shadow-decision-key")
	require.NoError(t, err)
	shadow := &ShadowInventoryService{projects: postgresRiskProjectResolver{queries: platformrepo.New(conn)}, inventory: stubShadowInventory{}, reviews: stubShadowReview{}, flags: flags, organizations: NewPostgresOrganizationSlugResolver(conn), budget: testOperationBudget(), references: references, versions: versions, now: time.Now}
	targetPayload := `{"kind":"server_url","key":"https://shadow.example.test/mcp"}`
	targetReference, err := shadow.references.EncodeScoped(principal, shadowTargetReferenceKind, queryScope("shadow_target", project.ID.String()), targetPayload, time.Now())
	require.NoError(t, err)
	review, err := core.ReadPlatformReview(ctx, mcpapproval.PlatformReviewReadInput{OrganizationID: principal.OrganizationID, ProjectID: project.ID, TargetKind: "server_url", TargetKey: "https://shadow.example.test/mcp"})
	require.NoError(t, err)
	version, err := shadow.versions.Encode(review.DecisionVersionState)
	require.NoError(t, err)
	service := NewShadowDecisionService(conn, shadow, core, testPluginTargets(conn), flags, NewPostgresOrganizationSlugResolver(conn), testOperationBudget())

	localRequest, err := approvalrepo.New(conn).UpsertApprovalRequest(ctx, approvalrepo.UpsertApprovalRequestParams{
		OrganizationID: principal.OrganizationID, ProjectID: project.ID,
		TargetKind: shadowTargetKindStdioCommand, TargetRaw: "local-command", TargetKey: "local-command",
		ArtifactRef: pgtype.Text{}, VersionPinned: false, Status: "requested", RiskPolicyBypassRequestID: uuid.NullUUID{},
	})
	require.NoError(t, err)
	localReference, err := shadow.references.EncodeScoped(principal, shadowTargetReferenceKind, queryScope("shadow_target", project.ID.String()), `{"kind":"stdio_command","key":"local-command"}`, time.Now())
	require.NoError(t, err)
	localReview, err := core.ReadPlatformReview(ctx, mcpapproval.PlatformReviewReadInput{OrganizationID: principal.OrganizationID, ProjectID: project.ID, TargetKind: shadowTargetKindStdioCommand, TargetKey: "local-command"})
	require.NoError(t, err)
	localVersion, err := shadow.versions.Encode(localReview.DecisionVersionState)
	require.NoError(t, err)
	_, err = service.Decide(ctx, principal, DecideShadowMCPAccessInput{ProjectID: project.ID.String(), TargetReference: localReference, Decision: "deny", Rationale: "not enforceable", AudienceReferences: []string{}, ExpectedVersion: localVersion, IdempotencyKey: "deny-local-shadow", Confirmed: true})
	var localErr *ShadowDecisionError
	require.ErrorAs(t, err, &localErr)
	require.Equal(t, "invalid_request", localErr.Code)
	localDecisions, err := approvalrepo.New(conn).ListDecisionsForApprovalRequest(ctx, approvalrepo.ListDecisionsForApprovalRequestParams{McpApprovalRequestID: localRequest.ID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Empty(t, localDecisions)

	denyAuditsBefore, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionMCPApprovalRequestDeny)
	require.NoError(t, err)
	input := DecideShadowMCPAccessInput{ProjectID: project.ID.String(), TargetReference: targetReference, Decision: "deny", Rationale: "not approved", AudienceReferences: []string{}, ExpectedVersion: version, IdempotencyKey: "deny-shadow", Confirmed: true}

	first, err := service.Decide(ctx, principal, input)
	require.NoError(t, err)
	require.False(t, first.Receipt.Replayed)
	replayed, err := service.Decide(ctx, principal, input)
	require.NoError(t, err)
	require.True(t, replayed.Receipt.Replayed)

	decisions, err := approvalrepo.New(conn).ListDecisionsForApprovalRequest(ctx, approvalrepo.ListDecisionsForApprovalRequestParams{McpApprovalRequestID: request.ID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Len(t, decisions, 1)
	require.Equal(t, "denied", decisions[0].Decision)
	stored, err := approvalrepo.New(conn).GetApprovalRequest(ctx, approvalrepo.GetApprovalRequestParams{ID: request.ID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Equal(t, "denied", stored.Status)
	require.False(t, stored.EvidenceChangedAt.Valid)
	denyAuditsAfter, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionMCPApprovalRequestDeny)
	require.NoError(t, err)
	require.Equal(t, denyAuditsBefore+1, denyAuditsAfter)
}

func TestShadowDecisionAllowsEveryoneAndReplaysAtomically(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_shadow_decision_allow")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	serverURL := "https://allowed-shadow.example.test/mcp"
	request, err := approvalrepo.New(conn).UpsertApprovalRequest(ctx, approvalrepo.UpsertApprovalRequestParams{
		OrganizationID: principal.OrganizationID, ProjectID: project.ID,
		TargetKind: shadowTargetKindServerURL, TargetRaw: serverURL, TargetKey: serverURL,
		ArtifactRef: pgtype.Text{}, VersionPinned: false, Status: "requested", RiskPolicyBypassRequestID: uuid.NullUUID{},
	})
	require.NoError(t, err)
	require.NoError(t, approvalrepo.New(conn).SetApprovalRequestEvidence(ctx, approvalrepo.SetApprovalRequestEvidenceParams{CurrentEvidence: []byte(`{"identity":{"kind":"remote"}}`), EvidenceVersion: 1, ID: request.ID, ProjectID: project.ID}))
	policyID := uuid.New()
	_, err = riskrepo.New(conn).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
		ID: policyID, ProjectID: project.ID, OrganizationID: principal.OrganizationID, Name: "shadow policy", Sources: []string{"shadow_mcp"},
		AnalyzerConfig: []byte(`{}`), DisabledRules: nil, Enabled: true, Action: "block", AudienceType: "everyone",
		ShadowMcpDisposition: conv.ToPGTextEmpty("block_all"), UserMessage: pgtype.Text{},
	})
	require.NoError(t, err)

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagMCPApproval, principal.OrganizationID, true)
	flags.SetFlag(feature.FlagPlatformMCPShadowAccessDecisions, principal.OrganizationID, true)
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	redisClient, err := platformMCPInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	sessions := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("shadow-decision-allow"), billing.NewStubClient(logger, tracerProvider))
	core := mcpapproval.NewService(logger, tracerProvider, conn, sessions, authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, nil), flags, audit.NewLogger(), nil, nil)
	references, err := newSubjectReferenceCodec("shadow-decision-allow-key")
	require.NoError(t, err)
	versions, err := newShadowDecisionVersionCodec("shadow-decision-allow-key")
	require.NoError(t, err)
	shadow := &ShadowInventoryService{projects: postgresRiskProjectResolver{queries: platformrepo.New(conn)}, inventory: stubShadowInventory{}, reviews: stubShadowReview{}, flags: flags, organizations: NewPostgresOrganizationSlugResolver(conn), budget: testOperationBudget(), references: references, versions: versions, now: time.Now}
	targetReference, err := shadow.references.EncodeScoped(principal, shadowTargetReferenceKind, queryScope("shadow_target", project.ID.String()), `{"kind":"server_url","key":"https://allowed-shadow.example.test/mcp"}`, time.Now())
	require.NoError(t, err)
	review, err := core.ReadPlatformReview(ctx, mcpapproval.PlatformReviewReadInput{OrganizationID: principal.OrganizationID, ProjectID: project.ID, TargetKind: shadowTargetKindServerURL, TargetKey: serverURL})
	require.NoError(t, err)
	version, err := shadow.versions.Encode(review.DecisionVersionState)
	require.NoError(t, err)
	plugins := testPluginTargets(conn)
	choices, err := plugins.ListPluginAssignments(ctx, principal, ListPluginAssignmentsInput{ProjectID: project.ID.String()})
	require.NoError(t, err)
	require.NotEmpty(t, choices.Assignments)
	require.Equal(t, "everyone", choices.Assignments[0].Kind)
	service := NewShadowDecisionService(conn, shadow, core, plugins, flags, NewPostgresOrganizationSlugResolver(conn), testOperationBudget())
	input := DecideShadowMCPAccessInput{ProjectID: project.ID.String(), TargetReference: targetReference, Decision: "allow", Rationale: "approved for everyone", AudienceReferences: []string{choices.Assignments[0].Reference}, ExpectedVersion: version, IdempotencyKey: "allow-shadow", Confirmed: true}
	approveAuditsBefore, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionMCPApprovalRequestApprove)
	require.NoError(t, err)

	first, err := service.Decide(ctx, principal, input)
	require.NoError(t, err)
	require.False(t, first.Receipt.Replayed)
	replayed, err := service.Decide(ctx, principal, input)
	require.NoError(t, err)
	require.True(t, replayed.Receipt.Replayed)

	decisions, err := approvalrepo.New(conn).ListDecisionsForApprovalRequest(ctx, approvalrepo.ListDecisionsForApprovalRequestParams{McpApprovalRequestID: request.ID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Len(t, decisions, 1)
	require.Equal(t, "approved", decisions[0].Decision)
	require.Equal(t, []string{authz.AllUsersPrincipal().String()}, decisions[0].GrantedPrincipalUrns)
	grants, err := authz.ListGrantsForResource(ctx, conn, authz.Resource{OrganizationID: principal.OrganizationID, Scope: authz.ScopeRiskPolicyBypass, ResourceID: policyID.String()})
	require.NoError(t, err)
	require.Len(t, grants, 1)
	require.Equal(t, authz.AllUsersPrincipal().String(), grants[0].PrincipalUrn)
	require.Equal(t, serverURL, grants[0].Selector[authz.SelectorKeyServerURL])
	approveAuditsAfter, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionMCPApprovalRequestApprove)
	require.NoError(t, err)
	require.Equal(t, approveAuditsBefore+1, approveAuditsAfter)
}
