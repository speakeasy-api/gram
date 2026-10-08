package platformmcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	approvalrepo "github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
)

const shadowReviewFixtureURL = "https://fixture.invalid/mcp"

// seedReadyDirectRemoteDistributionTarget is seedReadyDistributionTarget for a
// user-supplied URL: the registration carries the direct-remote provider, which
// is what makes the Shadow MCP policy apply to it at all.
func seedReadyDirectRemoteDistributionTarget(t *testing.T, ctx context.Context, conn *pgxpool.Pool) (Principal, ResolvedProject) {
	t.Helper()

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn)
	require.NoError(t, err)
	request := CatalogRegistrationRequest{
		ProjectSlug:      project.Slug,
		SourceKind:       directRemoteSourceKind,
		CatalogProvider:  directRemoteProviderKey,
		CatalogReference: shadowReviewFixtureURL,
		IdempotencyKey:   "direct-remote-review-registration",
		InputHash:        catalogRegistrationInputHash(project.Slug, directRemoteSourceKind, directRemoteProviderKey, shadowReviewFixtureURL),
	}
	receipt, err := store.BeginReceipt(ctx, principal, project, request, time.Now().UTC())
	require.NoError(t, err)
	receipt, err = store.ConvergeRegistration(ctx, principal, project, request, receipt)
	require.NoError(t, err)
	_, err = store.CompleteRegistrationWithRemoteURL(ctx, principal, project, request, receipt, shadowReviewFixtureURL)
	require.NoError(t, err)
	registration, err := platformrepo.New(conn).GetActivePlatformMCPCatalogRegistration(ctx, platformrepo.GetActivePlatformMCPCatalogRegistrationParams{
		OrganizationID:   principal.OrganizationID,
		ProjectID:        project.ID,
		SourceKind:       request.SourceKind,
		CatalogProvider:  request.CatalogProvider,
		CatalogReference: request.CatalogReference,
	})
	require.NoError(t, err)
	onboarding := NewOnboardingService(conn)
	_, err = onboarding.Start(ctx, principal.OrganizationID, principal.UserID)
	require.NoError(t, err)
	_, err = onboarding.BindRegistration(ctx, principal.OrganizationID, principal.UserID, project.ID, registration.ID)
	require.NoError(t, err)
	_, err = store.RecordReadiness(ctx, principal, ReadinessBinding{ProjectID: project.ID, RegistrationID: registration.ID, ProviderAuthorizationFingerprint: "fixture-readiness"}, ReadinessReady, "fixture", time.Now().UTC(), time.Now().UTC().Add(time.Hour))
	require.NoError(t, err)
	return principal, project
}

func seedBlockingShadowMCPPolicy(t *testing.T, ctx context.Context, conn *pgxpool.Pool, principal Principal, project ResolvedProject, name string) {
	t.Helper()
	_, err := riskrepo.New(conn).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
		ID: uuid.New(), ProjectID: project.ID, OrganizationID: principal.OrganizationID, Name: name, PolicyType: "standard",
		Sources: []string{shadowmcp.SourceShadowMCP}, PresidioEntities: nil, AnalyzerConfig: nil, PromptInjectionRules: nil, DisabledRules: nil, CustomRuleIds: nil,
		Enabled: true, Action: "block", AudienceType: "everyone", ShadowMcpDisposition: pgtype.Text{}, AutoName: false, UserMessage: pgtype.Text{}, Prompt: pgtype.Text{}, ModelConfig: nil, Score: pgtype.Float8{},
	})
	require.NoError(t, err)
}

func seedApprovedShadowMCPDecision(t *testing.T, ctx context.Context, conn *pgxpool.Pool, principal Principal, project ResolvedProject, canonicalURL string, principals []string) {
	t.Helper()
	request, err := approvalrepo.New(conn).UpsertApprovalRequest(ctx, approvalrepo.UpsertApprovalRequestParams{
		OrganizationID: principal.OrganizationID, ProjectID: project.ID, TargetKind: "server_url", TargetRaw: canonicalURL, TargetKey: canonicalURL,
		ArtifactRef: pgtype.Text{}, VersionPinned: true, Status: "requested", RiskPolicyBypassRequestID: uuid.NullUUID{},
	})
	require.NoError(t, err)
	_, err = approvalrepo.New(conn).CreateApprovalDecision(ctx, approvalrepo.CreateApprovalDecisionParams{
		OrganizationID: principal.OrganizationID, ProjectID: project.ID, McpApprovalRequestID: request.ID, Decision: "approved", DecidedBy: principal.UserID,
		Rationale: pgtype.Text{String: "approved for test", Valid: true}, EvidenceSnapshot: []byte(`{}`), EvidenceVersion: 1, GrantedPrincipalUrns: principals, McpResearchReportID: uuid.NullUUID{},
	})
	require.NoError(t, err)
}

// The policy, not a flag, decides. With a block policy and an audience the URL
// is not approved for, distribution writes nothing and files a review; with an
// approved decision for that audience the same call attaches the server.
func TestDistributionFilesShadowMCPReviewWhenTheProjectPolicyRefusesTheURL(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_shadow_review_distribution")
	require.NoError(t, err)
	principal, project := seedReadyDirectRemoteDistributionTarget(t, ctx, conn)
	plugin := seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Support", "support")
	_, err = pluginsrepo.New(conn).AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{PluginID: plugin.ID, OrganizationID: principal.OrganizationID, PrincipalUrn: "role:developers"})
	require.NoError(t, err)
	seedBlockingShadowMCPPolicy(t, ctx, conn, principal, project, "Block unreviewed MCPs")
	canonical, ok := shadowmcp.CanonicalizeInventoryURL(shadowReviewFixtureURL)
	require.True(t, ok)

	filer := &recordingShadowMCPReviewFiler{review: ShadowMCPReviewRequest{RequestID: "request-1", Status: "requested", Target: canonical.CanonicalURL, Explanation: "why"}}
	service := NewDistributionService(conn, nil, testExistingPluginAttacher(), func(context.Context, uuid.UUID, string, string) error { return nil }, testPluginTargets(conn)).
		WithDistributionAdmission(admission.NewGuard()).
		WithShadowMCPReview(filer)

	_, err = service.Distribute(ctx, principal, DistributionInput{ProjectSlug: project.Slug, Plugin: plugin.Slug, ExpectedVersion: 0, Justification: "support rota"})
	require.ErrorIs(t, err, ErrShadowMCPReviewRequired)
	require.ErrorIs(t, err, ErrDistributionBlockedPendingApproval)
	var review *ShadowMCPReviewRequiredError
	require.ErrorAs(t, err, &review)
	require.Equal(t, "request-1", review.Review.RequestID)
	require.Equal(t, 1, filer.calls)
	require.Equal(t, project.ID, filer.project.ID)
	require.Equal(t, canonical.CanonicalURL, filer.url)
	require.Equal(t, "adding "+canonical.CanonicalURL+" to the Support plugin in project "+project.Slug, filer.activity)
	require.Equal(t, "support rota", filer.justification)
	servers, err := pluginsrepo.New(conn).ListPluginServers(ctx, plugin.ID)
	require.NoError(t, err)
	require.Empty(t, servers, "nothing reaches the plugin while the review is pending")
	current, err := service.Current(ctx, principal, project.Slug, plugin.Slug)
	require.NoError(t, err)
	require.False(t, current.AttachmentLive)

	// Without a filer the same situation is still a refusal.
	_, err = NewDistributionService(conn, nil, testExistingPluginAttacher(), nil, testPluginTargets(conn)).WithDistributionAdmission(admission.NewGuard()).
		Distribute(ctx, principal, DistributionInput{ProjectSlug: project.Slug, Plugin: plugin.Slug, ExpectedVersion: 0})
	require.ErrorIs(t, err, ErrDistributionBlockedPendingApproval)
	require.False(t, errors.As(err, &review))

	// Once the audience is approved the same call attaches the server.
	seedApprovedShadowMCPDecision(t, ctx, conn, principal, project, canonical.CanonicalURL, []string{"role:developers"})
	attached, err := service.Distribute(ctx, principal, DistributionInput{ProjectSlug: project.Slug, Plugin: plugin.Slug, ExpectedVersion: 0})
	require.NoError(t, err)
	require.True(t, attached.AttachmentLive)
	require.Equal(t, 1, filer.calls, "an admitted distribution files nothing")
}

// A direct-remote server in a project with no block policy is never refused,
// which is what "the policy decides" means in the other direction.
func TestDistributionAdmitsADirectRemoteURLWithoutABlockPolicy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_shadow_review_open")
	require.NoError(t, err)
	principal, project := seedReadyDirectRemoteDistributionTarget(t, ctx, conn)
	plugin := seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Support", "support")
	_, err = pluginsrepo.New(conn).AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{PluginID: plugin.ID, OrganizationID: principal.OrganizationID, PrincipalUrn: "role:developers"})
	require.NoError(t, err)

	filer := &recordingShadowMCPReviewFiler{}
	service := NewDistributionService(conn, nil, testExistingPluginAttacher(), func(context.Context, uuid.UUID, string, string) error { return nil }, testPluginTargets(conn)).
		WithDistributionAdmission(admission.NewGuard()).
		WithShadowMCPReview(filer)
	attached, err := service.Distribute(ctx, principal, DistributionInput{ProjectSlug: project.Slug, Plugin: plugin.Slug, ExpectedVersion: 0})
	require.NoError(t, err)
	require.True(t, attached.AttachmentLive)
	require.Zero(t, filer.calls)
}

// The registration-time policy check and the review service's policy names
// read the same rows the runtime scanner enforces.
func TestPostgresDirectRemotePolicyAndPolicyNamesReadTheProjectsBlockPolicies(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_shadow_review_policy")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	policy := NewPostgresDirectRemotePolicy(conn)

	state, err := policy.CheckDirectRemotePolicy(ctx, principal, project, shadowReviewFixtureURL)
	require.NoError(t, err)
	require.False(t, state.EnforcementActive)
	require.True(t, state.Approved)
	names, err := postgresShadowMCPPolicyNames(conn)(ctx, project.ID)
	require.NoError(t, err)
	require.Empty(t, names)

	seedBlockingShadowMCPPolicy(t, ctx, conn, principal, project, "Zeta lockdown")
	seedBlockingShadowMCPPolicy(t, ctx, conn, principal, project, "Alpha lockdown")
	state, err = policy.CheckDirectRemotePolicy(ctx, principal, project, shadowReviewFixtureURL)
	require.NoError(t, err)
	require.True(t, state.EnforcementActive)
	require.False(t, state.Approved)
	names, err = postgresShadowMCPPolicyNames(conn)(ctx, project.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"Alpha lockdown", "Zeta lockdown"}, names)

	_, err = (*PostgresDirectRemotePolicy)(nil).CheckDirectRemotePolicy(ctx, principal, project, shadowReviewFixtureURL)
	require.ErrorIs(t, err, ErrRegistrationUnavailable)
}
