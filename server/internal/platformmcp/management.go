package platformmcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	platformmcphttp "github.com/speakeasy-api/gram/server/gen/http/platform_mcp/server"
	platformmcpgen "github.com/speakeasy-api/gram/server/gen/platform_mcp"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

const (
	repairActionEnablePlatformMCP    = "enable_platform_mcp"
	repairActionAuthorizePlatformMCP = "authorize_platform_mcp"
	repairActionSelectProject        = "select_project"
	repairActionStartSetup           = "start_setup"
	repairActionPublication          = "repair_publication"
)

// ManagementService exposes the session-authenticated dashboard projection. It
// deliberately returns only bounded lifecycle state; setup handoffs, OAuth
// values, provider material, and internal resource identifiers stay private.
type ManagementService struct {
	auth          *auth.Auth
	db            *pgxpool.Pool
	authorizer    Authorizer
	gate          Gate
	onboarding    *OnboardingService
	registrations *RegistrationService
	readiness     *ReadinessService
	distributions *DistributionService
	versionTokens *distributionVersionTokenCodec
	catalog       Catalog
	mcpURL        string
	tracer        trace.Tracer
}

var _ platformmcpgen.Service = (*ManagementService)(nil)
var _ platformmcpgen.Auther = (*ManagementService)(nil)

func NewManagementService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessionManager *sessions.Manager, authzEngine *authz.Engine, gate Gate, authorizer Authorizer, mcpURL string, registrations *RegistrationService, readiness *ReadinessService, distributions *DistributionService, versionTokenKey string, catalog Catalog) *ManagementService {
	versionTokens, _ := newDistributionVersionTokenCodec(versionTokenKey)
	return &ManagementService{
		auth:          auth.New(logger, db, sessionManager, authzEngine),
		db:            db,
		authorizer:    authorizer,
		gate:          gate,
		onboarding:    NewOnboardingService(db),
		registrations: registrations,
		readiness:     readiness,
		distributions: distributions,
		versionTokens: versionTokens,
		catalog:       catalog,
		mcpURL:        mcpURL,
		tracer:        tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/platformmcp"),
	}
}

func AttachManagement(mux goahttp.Muxer, service *ManagementService) {
	endpoints := platformmcpgen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))

	httpServer := platformmcphttp.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil)
	httpServer.Use(noStoreHeaders)
	platformmcphttp.Mount(mux, httpServer)
}

func (s *ManagementService) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	if s == nil || s.auth == nil {
		return ctx, oops.C(oops.CodeUnexpected)
	}
	return s.auth.Authorize(ctx, key, schema)
}

func (s *ManagementService) GetOnboarding(ctx context.Context, _ *platformmcpgen.GetOnboardingPayload) (*platformmcpgen.PlatformMCPOnboardingState, error) {
	authCtx, err := s.authorizedContext(ctx)
	if err != nil {
		return nil, err
	}
	enabled, err := s.enabled(ctx, authCtx)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return s.disabledState(), nil
	}
	projection, err := s.onboarding.Get(ctx, authCtx.ActiveOrganizationID, authCtx.UserID)
	if err != nil {
		return nil, s.mapOnboardingError(err)
	}
	readiness, found := s.currentReadiness(ctx, authCtx, projection)
	return s.state(ctx, authCtx, projection, true, readiness, found), nil
}

func (s *ManagementService) StartOnboarding(ctx context.Context, _ *platformmcpgen.StartOnboardingPayload) (*platformmcpgen.PlatformMCPOnboardingState, error) {
	authCtx, err := s.enabledContext(ctx)
	if err != nil {
		return nil, err
	}
	projection, err := s.onboarding.Start(ctx, authCtx.ActiveOrganizationID, authCtx.UserID)
	if err != nil {
		return nil, s.mapOnboardingError(err)
	}
	return s.state(ctx, authCtx, projection, true, nil, false), nil
}

func (s *ManagementService) RecordInstallIntent(ctx context.Context, payload *platformmcpgen.RecordInstallIntentPayload) (*platformmcpgen.PlatformMCPOnboardingState, error) {
	if payload == nil {
		return nil, oops.C(oops.CodeBadRequest)
	}
	authCtx, err := s.enabledContext(ctx)
	if err != nil {
		return nil, err
	}
	projection, err := s.onboarding.RecordInstallIntent(ctx, authCtx.ActiveOrganizationID, authCtx.UserID, OnboardingClientFamily(payload.ClientFamily))
	if err != nil {
		return nil, s.mapOnboardingError(err)
	}
	return s.state(ctx, authCtx, projection, true, nil, false), nil
}

func (s *ManagementService) RecordAgentConfigurationCopied(ctx context.Context, _ *platformmcpgen.RecordAgentConfigurationCopiedPayload) (*platformmcpgen.PlatformMCPOnboardingState, error) {
	authCtx, err := s.enabledContext(ctx)
	if err != nil {
		return nil, err
	}
	projection, err := s.onboarding.RecordAgentConfigurationCopied(ctx, authCtx.ActiveOrganizationID, authCtx.UserID)
	if err != nil {
		return nil, s.mapOnboardingError(err)
	}
	readiness, found := s.currentReadiness(ctx, authCtx, projection)
	return s.state(ctx, authCtx, projection, true, readiness, found), nil
}

func (s *ManagementService) StartOnboardingSetup(ctx context.Context, _ *platformmcpgen.StartOnboardingSetupPayload) (*platformmcpgen.PlatformMCPOnboardingSetupHandoff, error) {
	authCtx, err := s.enabledContext(ctx)
	if err != nil {
		return nil, err
	}
	projection, err := s.onboarding.Get(ctx, authCtx.ActiveOrganizationID, authCtx.UserID)
	if err != nil {
		return nil, s.mapOnboardingError(err)
	}
	principal, err := s.currentConnectionPrincipal(authCtx, projection)
	if err != nil {
		return nil, s.mapOnboardingError(err)
	}
	if s.catalog == nil || s.registrations == nil || projection.Workflow == nil || projection.SelectedProject == nil || projection.Workflow.SelectedRegistrationID == uuid.Nil {
		return nil, oops.C(oops.CodeBadRequest)
	}
	candidate, err := s.registrations.store.ResolveRegistrationCatalogIdentity(ctx, principal, *projection.SelectedProject, projection.Workflow.SelectedRegistrationID)
	if err != nil {
		return nil, s.mapOnboardingError(err)
	}
	if isBrowserCatalogProviderKey(candidate.ProviderKey) {
		setupURL, err := s.registrations.DashboardSetupURL(ctx, principal, IssueSetupHandoffInput{ProjectSlug: projection.SelectedProject.Slug, RegistrationID: projection.Workflow.SelectedRegistrationID.String(), ProviderKey: candidate.ProviderKey, CatalogRef: candidate.CatalogRef})
		if err != nil {
			return nil, s.mapOnboardingError(err)
		}
		return &platformmcpgen.PlatformMCPOnboardingSetupHandoff{DashboardSetupURL: &setupURL}, nil
	}
	issued, err := s.registrations.IssueSetupHandoffForRegistration(ctx, principal, projection.SelectedProject.Slug, projection.Workflow.SelectedRegistrationID.String())
	if err != nil {
		return nil, s.mapOnboardingError(err)
	}
	return &platformmcpgen.PlatformMCPOnboardingSetupHandoff{Handoff: &issued.Value}, nil
}

func (s *ManagementService) RecheckOnboardingReadiness(ctx context.Context, _ *platformmcpgen.RecheckOnboardingReadinessPayload) (*platformmcpgen.PlatformMCPOnboardingState, error) {
	authCtx, err := s.enabledContext(ctx)
	if err != nil {
		return nil, err
	}
	projection, err := s.onboarding.Get(ctx, authCtx.ActiveOrganizationID, authCtx.UserID)
	if err != nil {
		return nil, s.mapOnboardingError(err)
	}
	principal, err := s.currentConnectionPrincipal(authCtx, projection)
	if err != nil {
		return nil, s.mapOnboardingError(err)
	}
	if s.readiness == nil || projection.Workflow == nil || projection.SelectedProject == nil || projection.Workflow.SelectedRegistrationID == uuid.Nil {
		return nil, oops.C(oops.CodeBadRequest)
	}
	_, readiness, found, err := s.readiness.GetReadiness(ctx, principal, projection.SelectedProject.Slug, projection.Workflow.SelectedRegistrationID.String(), true)
	if err != nil {
		return nil, s.mapOnboardingError(err)
	}
	return s.state(ctx, authCtx, projection, true, &readiness, found), nil
}

func (s *ManagementService) DistributeOnboardingCandidate(ctx context.Context, payload *platformmcpgen.DistributeOnboardingCandidatePayload) (*platformmcpgen.PlatformMCPOnboardingState, error) {
	if payload == nil {
		return nil, oops.C(oops.CodeBadRequest)
	}
	return s.mutateDistribution(ctx, payload.ProjectSlug, payload.ExpectedVersion, func(service *DistributionService, principal Principal, projectSlug string, expectedVersion int64) error {
		_, err := service.Distribute(ctx, principal, DistributionInput{ProjectSlug: projectSlug, ExpectedVersion: expectedVersion})
		return err
	})
}
func (s *ManagementService) RemoveOnboardingDistribution(ctx context.Context, payload *platformmcpgen.RemoveOnboardingDistributionPayload) (*platformmcpgen.PlatformMCPOnboardingState, error) {
	if payload == nil {
		return nil, oops.C(oops.CodeBadRequest)
	}
	return s.mutateDistribution(ctx, payload.ProjectSlug, payload.ExpectedVersion, func(service *DistributionService, principal Principal, projectSlug string, expectedVersion int64) error {
		_, err := service.Remove(ctx, principal, DistributionInput{ProjectSlug: projectSlug, ExpectedVersion: expectedVersion})
		return err
	})
}
func (s *ManagementService) RepairOnboardingPublication(ctx context.Context, payload *platformmcpgen.RepairOnboardingPublicationPayload) (*platformmcpgen.PlatformMCPOnboardingState, error) {
	if payload == nil {
		return nil, oops.C(oops.CodeBadRequest)
	}
	return s.mutateDistribution(ctx, payload.ProjectSlug, payload.ExpectedVersion, func(service *DistributionService, principal Principal, projectSlug string, expectedVersion int64) error {
		_, err := service.RepairPublication(ctx, principal, DistributionInput{ProjectSlug: projectSlug, ExpectedVersion: expectedVersion})
		return err
	})
}

type distributionMutation func(*DistributionService, Principal, string, int64) error

func (s *ManagementService) mutateDistribution(ctx context.Context, projectSlug, expectedVersionToken string, mutate distributionMutation) (*platformmcpgen.PlatformMCPOnboardingState, error) {
	if projectSlug == "" || expectedVersionToken == "" {
		return nil, oops.C(oops.CodeBadRequest)
	}
	authCtx, err := s.enabledContext(ctx)
	if err != nil {
		return nil, err
	}
	projection, err := s.onboarding.Get(ctx, authCtx.ActiveOrganizationID, authCtx.UserID)
	if err != nil {
		return nil, s.mapOnboardingError(err)
	}
	principal, err := s.currentConnectionPrincipal(authCtx, projection)
	if err != nil || s.distributions == nil || s.versionTokens == nil || projection.SelectedProject == nil {
		return nil, oops.C(oops.CodeBadRequest)
	}
	expectedVersion, err := s.versionTokens.Decode(expectedVersionToken, principal, projectSlug)
	if err != nil {
		return nil, oops.C(oops.CodeBadRequest)
	}
	if err := mutate(s.distributions, principal, projectSlug, expectedVersion); err != nil {
		return nil, s.mapOnboardingError(err)
	}
	readiness, found := s.currentReadiness(ctx, authCtx, projection)
	return s.state(ctx, authCtx, projection, true, readiness, found), nil
}

func (s *ManagementService) DismissOnboarding(ctx context.Context, _ *platformmcpgen.DismissOnboardingPayload) error {
	authCtx, err := s.enabledContext(ctx)
	if err != nil {
		return err
	}
	if err := s.onboarding.Dismiss(ctx, authCtx.ActiveOrganizationID, authCtx.UserID); err != nil {
		return s.mapOnboardingError(err)
	}
	return nil
}

func (s *ManagementService) authorizedContext(ctx context.Context) (*contextvalues.AuthContext, error) {
	if s == nil || s.authorizer == nil || s.gate == nil || s.onboarding == nil {
		return nil, oops.C(oops.CodeUnexpected)
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.UserID == "" || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	err := s.authorizer.RequireLiveOrgAdmin(ctx, Principal{UserID: authCtx.UserID, OrganizationID: authCtx.ActiveOrganizationID})
	if err == nil {
		return authCtx, nil
	}
	if isAuthorizationDenied(err) {
		return nil, oops.C(oops.CodeForbidden)
	}
	return nil, oops.E(oops.CodeUnexpected, err, "authorize platform mcp management access")
}
func (s *ManagementService) enabledContext(ctx context.Context) (*contextvalues.AuthContext, error) {
	authCtx, err := s.authorizedContext(ctx)
	if err != nil {
		return nil, err
	}
	enabled, err := s.enabled(ctx, authCtx)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, oops.C(oops.CodeForbidden)
	}
	return authCtx, nil
}
func (s *ManagementService) enabled(ctx context.Context, authCtx *contextvalues.AuthContext) (bool, error) {
	enabled, err := s.gate.Enabled(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return false, oops.E(oops.CodeUnexpected, err, "check platform mcp management gate")
	}
	return enabled, nil
}

func (s *ManagementService) disabledState() *platformmcpgen.PlatformMCPOnboardingState {
	return &platformmcpgen.PlatformMCPOnboardingState{Enabled: false, Stage: string(OnboardingStageNotStarted), McpURL: s.mcpURL, WorkflowActive: false, ClientFamily: "", AgentConfigurationCopied: false, ConnectionAuthorized: false, ConnectionReady: false, CatalogExplored: false, SelectedProjectName: "", SelectedProjectSlug: "", RegistrationComplete: false, ReadinessState: "", ReadinessFreshness: "", DistributionState: "", DistributionAttached: false, DistributionToolSucceeded: false, ReadinessVerified: false, DistributionPublicationState: "", SelectedUseVerified: false, DistributionExpectedVersion: "", RepairAction: repairActionEnablePlatformMCP}
}

func (s *ManagementService) state(ctx context.Context, authCtx *contextvalues.AuthContext, projection OnboardingProjection, enabled bool, readiness *Readiness, readinessFound bool) *platformmcpgen.PlatformMCPOnboardingState {
	connectionReady := false
	for _, connection := range projection.Connections {
		connectionReady = connectionReady || connection.Ready
	}
	clientFamily := ""
	workflowActive := projection.Workflow != nil
	if projection.Workflow != nil {
		clientFamily = string(projection.Workflow.ClientFamily)
	}
	selectedProjectName, selectedProjectSlug, readinessState, freshness := "", "", "", ""
	registrationComplete := false
	if projection.SelectedProject != nil {
		selectedProjectName = projection.SelectedProject.Name
		selectedProjectSlug = projection.SelectedProject.Slug
	}
	if projection.Workflow != nil && projection.Workflow.SelectedRegistrationID != uuid.Nil {
		registrationComplete = true
	}
	if readiness != nil {
		readinessState = string(readiness.State)
		freshness = readinessFreshness(*readiness, readinessFound)
	}
	distributionState, distributionPublicationState, distributionExpectedVersion := "", "", ""
	distributionAttached, selectedUseVerified, distributionAvailable := false, false, false
	readinessVerified := readiness != nil && readinessFound && readiness.State == ReadinessReady && readinessFreshness(*readiness, readinessFound) == "fresh"
	if enabled && authCtx != nil && projection.SelectedProject != nil && projection.Workflow != nil && projection.Workflow.SelectedRegistrationID != uuid.Nil {
		if distribution, available := s.currentDistribution(ctx, authCtx, projection); available {
			distributionAvailable = true
			distributionState = distribution.State
			distributionAttached = distribution.AttachmentLive
			distributionPublicationState = distribution.PublicationState
			selectedUseVerified = s.hasSelectedUseEvidence(ctx, authCtx, projection)
			if s.versionTokens != nil {
				if principal, err := s.currentConnectionPrincipal(authCtx, projection); err == nil {
					distributionExpectedVersion, _ = s.versionTokens.Encode(principal, projection.SelectedProject.Slug, distribution.Version)
				}
			}
		}
	}
	repairAction := ""
	switch {
	case distributionAvailable && distributionPublicationState == publicationStateRepairRequired:
		repairAction = repairActionPublication
	case workflowActive && !connectionReady:
		repairAction = repairActionAuthorizePlatformMCP
	case workflowActive && !registrationComplete:
		repairAction = repairActionSelectProject
	case registrationComplete && readiness != nil && readiness.State == ReadinessReady && readinessFound && !distributionAttached:
		if distributionAvailable {
			repairAction = "distribute_to_default_plugin"
		} else {
			repairAction = "contact_support"
		}
	case registrationComplete && readiness != nil:
		actions := repairActions(readiness.State)
		if len(actions) > 0 {
			repairAction = actions[0].Kind
		}
	case registrationComplete:
		repairAction = repairActionStartSetup
	}
	return &platformmcpgen.PlatformMCPOnboardingState{Enabled: enabled, Stage: string(projection.Stage), McpURL: s.mcpURL, WorkflowActive: workflowActive, ClientFamily: clientFamily, AgentConfigurationCopied: agentConfigurationReady(projection), ConnectionAuthorized: len(projection.Connections) > 0, ConnectionReady: connectionReady, CatalogExplored: projection.CatalogExplored, SelectedProjectName: selectedProjectName, SelectedProjectSlug: selectedProjectSlug, RegistrationComplete: registrationComplete, ReadinessState: readinessState, ReadinessFreshness: freshness, DistributionState: distributionState, DistributionAttached: distributionAttached, DistributionToolSucceeded: projection.DistributionToolSucceeded, ReadinessVerified: readinessVerified || projection.ReadinessVerified, DistributionPublicationState: distributionPublicationState, SelectedUseVerified: selectedUseVerified, DistributionExpectedVersion: distributionExpectedVersion, RepairAction: repairAction}
}
func agentConfigurationReady(projection OnboardingProjection) bool {
	if projection.Workflow == nil {
		return false
	}
	return projection.Workflow.AgentConfigurationCopiedAt != nil || len(projection.Connections) > 0 || projection.CatalogExplored || projection.RegistrationSucceeded || projection.DistributionToolSucceeded || projection.ReadinessVerified
}
func (s *ManagementService) hasSelectedUseEvidence(ctx context.Context, authCtx *contextvalues.AuthContext, projection OnboardingProjection) bool {
	if authCtx == nil || projection.Workflow == nil || projection.SelectedProject == nil || projection.Workflow.SelectedRegistrationID == uuid.Nil || s.db == nil {
		return false
	}
	verified, err := repo.New(s.db).HasPlatformMCPSelectedUseEvidence(ctx, repo.HasPlatformMCPSelectedUseEvidenceParams{InitiatingSubjectUrn: userSubjectURN(authCtx.UserID), OrganizationID: authCtx.ActiveOrganizationID, ProjectID: projection.SelectedProject.ID, RegistrationID: projection.Workflow.SelectedRegistrationID})
	return err == nil && verified
}
func (s *ManagementService) currentDistribution(ctx context.Context, authCtx *contextvalues.AuthContext, projection OnboardingProjection) (Distribution, bool) {
	if s.distributions == nil || authCtx == nil || projection.SelectedProject == nil {
		return Distribution{}, false
	}
	principal, err := s.currentConnectionPrincipal(authCtx, projection)
	if err != nil {
		return Distribution{}, false
	}
	distribution, err := s.distributions.Current(ctx, principal, projection.SelectedProject.Slug)
	if err != nil {
		return Distribution{}, false
	}
	return distribution, true
}
func (s *ManagementService) currentReadiness(ctx context.Context, authCtx *contextvalues.AuthContext, projection OnboardingProjection) (*Readiness, bool) {
	if s.readiness == nil || projection.Workflow == nil || projection.SelectedProject == nil || projection.Workflow.SelectedRegistrationID == uuid.Nil {
		return nil, false
	}
	principal, err := s.currentConnectionPrincipal(authCtx, projection)
	if err != nil {
		return nil, false
	}
	_, readiness, found, err := s.readiness.CurrentReadiness(ctx, principal, projection.SelectedProject.Slug, projection.Workflow.SelectedRegistrationID.String())
	if err != nil || !found {
		return nil, false
	}
	return &readiness, true
}
func (s *ManagementService) currentConnectionPrincipal(authCtx *contextvalues.AuthContext, projection OnboardingProjection) (Principal, error) {
	if authCtx == nil || authCtx.UserID == "" || authCtx.ActiveOrganizationID == "" || len(projection.Connections) == 0 {
		return Principal{}, ErrUnauthorized
	}
	connection := projection.Connections[0]
	if connection.ID == uuid.Nil || connection.Generation == uuid.Nil {
		return Principal{}, ErrUnauthorized
	}
	return Principal{UserID: authCtx.UserID, OrganizationID: authCtx.ActiveOrganizationID, ConnectionID: connection.ID.String(), Generation: connection.Generation.String()}, nil
}
func onboardingRegistrationIdempotencyKey(workflowID uuid.UUID, projectSlug, providerKey, catalogRef string) string {
	payload := workflowID.String() + "\x00" + projectSlug + "\x00" + providerKey + "\x00" + catalogRef
	digest := sha256.Sum256([]byte(payload))
	return "platform-mcp-onboarding-registration:" + hex.EncodeToString(digest[:])
}
func (s *ManagementService) mapOnboardingError(err error) error {
	switch {
	case errors.Is(err, ErrOnboardingInvalid), errors.Is(err, ErrRegistrationInvalid), errors.Is(err, ErrSetupHandoffInvalid), errors.Is(err, ErrReadinessInvalid), errors.Is(err, ErrReadinessRegistrationNotFound), errors.Is(err, ErrDistributionInvalid), errors.Is(err, ErrDistributionVersionTokenInvalid):
		return oops.C(oops.CodeBadRequest)
	case errors.Is(err, ErrDistributionConflict), errors.Is(err, ErrDistributionDefaultAbsent), errors.Is(err, ErrDistributionNotReady):
		return oops.C(oops.CodeConflict)
	case errors.Is(err, ErrForbidden), errors.Is(err, ErrTargetIneligible):
		return oops.C(oops.CodeForbidden)
	case errors.Is(err, ErrOperationRateLimited), errors.Is(err, ErrReadinessRateLimited):
		return oops.C(oops.CodeRateLimitExceeded)
	case errors.Is(err, ErrUnavailable), errors.Is(err, ErrRegistrationUnavailable), errors.Is(err, ErrOperationBudgetUnavailable):
		return oops.C(oops.CodeUnexpected)
	default:
		return oops.E(oops.CodeUnexpected, fmt.Errorf("platform mcp onboarding: %w", err), "load platform mcp onboarding")
	}
}
func noStoreHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		next.ServeHTTP(w, r)
	})
}
