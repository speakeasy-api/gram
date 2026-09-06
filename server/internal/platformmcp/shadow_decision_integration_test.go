package platformmcp

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
	approvalrepo "github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
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
}
