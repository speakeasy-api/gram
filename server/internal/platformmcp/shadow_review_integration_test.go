package platformmcp

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
	approvalrepo "github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
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
	principal.Surface = SurfacePlatformMCP
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

// newShadowApprovalDistribution wires a distribution service the way the server
// does: the real approval service behind the guard, and the real admission
// reader behind the standing report.
func newShadowApprovalDistribution(t *testing.T, conn *pgxpool.Pool, suffix string) (*DistributionService, *mcpapproval.Service) {
	t.Helper()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	redisClient, err := platformMCPInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	sessions := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("shadow-approval-"+suffix), billing.NewStubClient(logger, tracerProvider))
	engine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, nil)
	core := mcpapproval.NewService(logger, tracerProvider, conn, sessions, engine, new(feature.InMemory), audit.NewLogger(), nil, nil)
	dashboard, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	organizations := NewPostgresOrganizationSlugResolver(conn)
	service := NewDistributionService(conn, nil, testExistingPluginAttacher(), func(context.Context, uuid.UUID, string, string) error { return nil }, testPluginTargets(conn)).
		WithDistributionAdmission(admission.NewGuard().WithApprover(core)).
		WithShadowMCPStanding(NewShadowDistributionReadService(logger, conn), NewShadowMCPReviewLinkService(dashboard, organizations)).
		WithAdministratorAuthorization(engine)
	return service, core
}

// signedInAs is the request context a Platform MCP or dashboard call arrives
// with: the acting user, enforced authorization, and exactly the grants given.
// Being an organization administrator is a grant, not a property of the
// surface or the principal.
func signedInAs(ctx context.Context, principal Principal, admin bool) context.Context {
	sessionID := "session-" + principal.UserID
	ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID, SessionID: &sessionID})
	if admin {
		return authztest.WithAdminGrants(ctx)
	}
	return authz.GrantsToContext(ctx, []authz.Grant{})
}

// An organization administrator distributing a server added by URL is the
// review the project's block policy asks for: the write succeeds, the server
// is recorded in Shadow MCP as approved for the plugin's audience, and the
// result says so. The same write by a non-administrator is still refused.
func TestDistributionByAnAdministratorIsRecordedAsTheShadowMCPApproval(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_shadow_admin_approval")
	require.NoError(t, err)
	principal, project := seedReadyDirectRemoteDistributionTarget(t, ctx, conn)
	plugin := seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Support", "support")
	_, err = pluginsrepo.New(conn).AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{PluginID: plugin.ID, OrganizationID: principal.OrganizationID, PrincipalUrn: urn.PrincipalWildcard})
	require.NoError(t, err)
	seedBlockingShadowMCPPolicy(t, ctx, conn, principal, project, "Block unreviewed MCPs")
	canonical, ok := shadowmcp.CanonicalizeInventoryURL(shadowReviewFixtureURL)
	require.True(t, ok)
	service, core := newShadowApprovalDistribution(t, conn, "admin")

	// A request without an acting user, and one from a signed-in member who is
	// not an organization administrator, keep being refused: the Platform MCP
	// surface alone proves nothing.
	_, err = service.Distribute(ctx, principal, DistributionInput{ProjectSlug: project.Slug, Plugin: plugin.Slug, ExpectedVersion: 0})
	require.ErrorIs(t, err, ErrDistributionBlockedPendingApproval)
	_, err = service.Distribute(signedInAs(ctx, principal, false), principal, DistributionInput{ProjectSlug: project.Slug, Plugin: plugin.Slug, ExpectedVersion: 0})
	require.ErrorIs(t, err, ErrDistributionBlockedPendingApproval)
	servers, err := pluginsrepo.New(conn).ListPluginServers(ctx, plugin.ID)
	require.NoError(t, err)
	require.Empty(t, servers)
	_, err = core.ResolveDecisionTarget(ctx, principal.OrganizationID, project.ID, "server_url", canonical.CanonicalURL)
	require.Error(t, err, "a refused write records nothing in Shadow MCP")

	// The acting user holds org:admin: the write is admitted and recorded as the approval.
	adminCtx := signedInAs(ctx, principal, true)
	attached, err := service.Distribute(adminCtx, principal, DistributionInput{ProjectSlug: project.Slug, Plugin: plugin.Slug, ExpectedVersion: 0})
	require.NoError(t, err)
	require.True(t, attached.AttachmentLive)
	require.NotNil(t, attached.ShadowMCP)
	require.Equal(t, shadowMCPStandingApproved, attached.ShadowMCP.State)
	require.Equal(t, canonical.CanonicalURL, attached.ShadowMCP.Target)
	require.Contains(t, attached.ShadowMCP.ReviewURL, "/projects/"+project.Slug+"/shadow-ai/mcps/"+shadowmcp.ServerSlug(canonical.CanonicalURL))

	requestID, err := core.ResolveDecisionTarget(ctx, principal.OrganizationID, project.ID, "server_url", canonical.CanonicalURL)
	require.NoError(t, err, "the server is now a Shadow MCP inventory entry")
	request, err := approvalrepo.New(conn).GetApprovalRequest(ctx, approvalrepo.GetApprovalRequestParams{ID: requestID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Equal(t, "approved", request.Status)
	decisions, err := approvalrepo.New(conn).ListDecisionsForApprovalRequest(ctx, approvalrepo.ListDecisionsForApprovalRequestParams{McpApprovalRequestID: requestID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Len(t, decisions, 1)
	require.Equal(t, "approved", decisions[0].Decision)
	require.Equal(t, principal.UserID, decisions[0].DecidedBy)
	require.Equal(t, []string{authz.AllUsersPrincipal().String()}, decisions[0].GrantedPrincipalUrns, "approved for the plugin's audience: everyone")
	requester, err := approvalrepo.New(conn).GetPlatformRequesterApprovalRequest(ctx, approvalrepo.GetPlatformRequesterApprovalRequestParams{ID: requestID, OrganizationID: principal.OrganizationID, ProjectID: project.ID, UserID: principal.UserID})
	require.NoError(t, err, "the administrator is the entry's requester")
	require.Equal(t, canonical.CanonicalURL, requester.TargetRaw)

	// Distributing again is covered by the standing approval and records
	// nothing new, and the replay reports the same standing as the first write.
	again, err := service.Distribute(adminCtx, principal, DistributionInput{ProjectSlug: project.Slug, Plugin: plugin.Slug, ExpectedVersion: attached.Version})
	require.NoError(t, err)
	require.True(t, again.AttachmentLive)
	require.NotNil(t, again.ShadowMCP, "a replay reports the standing too")
	require.Equal(t, shadowMCPStandingApproved, again.ShadowMCP.State)
	decisions, err = approvalrepo.New(conn).ListDecisionsForApprovalRequest(ctx, approvalrepo.ListDecisionsForApprovalRequestParams{McpApprovalRequestID: requestID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Len(t, decisions, 1)
}

// A direct-remote server in a project with no block policy is never refused
// and nothing is recorded, which is what "the policy decides" means in the
// other direction.
func TestDistributionAdmitsADirectRemoteURLWithoutABlockPolicy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_shadow_review_open")
	require.NoError(t, err)
	principal, project := seedReadyDirectRemoteDistributionTarget(t, ctx, conn)
	plugin := seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Support", "support")
	_, err = pluginsrepo.New(conn).AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{PluginID: plugin.ID, OrganizationID: principal.OrganizationID, PrincipalUrn: urn.PrincipalWildcard})
	require.NoError(t, err)
	service, core := newShadowApprovalDistribution(t, conn, "open")

	attached, err := service.Distribute(ctx, principal, DistributionInput{ProjectSlug: project.Slug, Plugin: plugin.Slug, ExpectedVersion: 0})
	require.NoError(t, err)
	require.True(t, attached.AttachmentLive)
	require.NotNil(t, attached.ShadowMCP)
	require.NotEqual(t, shadowMCPStandingApproved, attached.ShadowMCP.State)
	canonical, ok := shadowmcp.CanonicalizeInventoryURL(shadowReviewFixtureURL)
	require.True(t, ok)
	_, err = core.ResolveDecisionTarget(ctx, principal.OrganizationID, project.ID, "server_url", canonical.CanonicalURL)
	require.Error(t, err, "no policy, no approval to record")
}
