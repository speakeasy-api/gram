package customdomains

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	gen "github.com/speakeasy-api/gram/server/gen/domains"
	srv "github.com/speakeasy-api/gram/server/gen/http/domains/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"
)

type Service struct {
	tracer             trace.Tracer
	logger             *slog.Logger
	db                 *pgxpool.Pool
	auth               *auth.Auth
	authz              *authz.Engine
	temporalClient     TemporalClient
	audit              *audit.Logger
	networkIngressPins NetworkIngressPinChecker
	// expectedTargetCNAME and expectedARecords are the DNS targets surfaced to
	// customers: the CNAME target for subdomains and the static ingress IPs
	// for apex domains, which cannot carry a CNAME.
	expectedTargetCNAME string
	expectedARecords    []netip.Addr
}

type NetworkIngressPinChecker func(ctx context.Context, dbtx pgx.Tx, organizationID string, customDomainID uuid.UUID) (bool, error)

type TemporalClient interface {
	GetWorkflowInfo(ctx context.Context, orgID string, domain string, customDomainID uuid.UUID) (*workflowservice.DescribeWorkflowExecutionResponse, error)
	ExecuteCustomDomainRegistration(ctx context.Context, orgID string, domain string, customDomainID uuid.UUID, createdBy urn.Principal, createdByName *string, provisionerKind k8s.ProvisionerKind, ipAllowlist []string) (client.WorkflowRun, error)
	TerminateCustomDomainRegistration(ctx context.Context, orgID string, domain string, customDomainID uuid.UUID, reason string) error
	ExecuteCustomDomainDeletion(ctx context.Context, orgID, domain, ingressName, certSecretName string, provisionerKind k8s.ProvisionerKind) (client.WorkflowRun, error)
	ExecuteCustomDomainUpdate(ctx context.Context, orgID, domain string, provisionerKind k8s.ProvisionerKind, ipAllowlist []string) (client.WorkflowRun, error)
	ExecuteCustomDomainReconcile(ctx context.Context, customDomainID uuid.UUID) (client.WorkflowRun, error)
	ExecuteCustomDomainHealthCheck(ctx context.Context, organizationID string, customDomainID uuid.UUID) (client.WorkflowRun, error)
}

var _ gen.Service = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	temporal TemporalClient,
	authzEngine *authz.Engine,
	auditLogger *audit.Logger,
	networkIngressPins NetworkIngressPinChecker,
	expectedTargetCNAME string,
	expectedARecords []netip.Addr,
) *Service {
	logger = logger.With(attr.SlogComponent("custom_domains"))

	return &Service{
		tracer:              tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/customdomains"),
		logger:              logger,
		db:                  db,
		auth:                auth.New(logger, db, sessions, authzEngine),
		authz:               authzEngine,
		temporalClient:      temporal,
		audit:               auditLogger,
		networkIngressPins:  networkIngressPins,
		expectedTargetCNAME: expectedTargetCNAME,
		expectedARecords:    expectedARecords,
	}
}

// domainView builds the API view of a custom domain, annotated with the DNS
// record-type suggestion for its hostname. The suggestion is a publicsuffix
// heuristic: an apex-looking domain gets "a" only when static ingress IPs are
// configured, everything else gets "cname".
func (s *Service) domainView(domain repo.CustomDomain, isUpdating bool, rootMcpEndpointID uuid.UUID) *gen.CustomDomain {
	view := mv.BuildCustomDomainView(domain, isUpdating, rootMcpEndpointID)
	suggested := "cname"
	if len(s.expectedARecords) > 0 && IsProbablyApexDomain(domain.Domain) {
		suggested = "a"
	}
	view.SuggestedRecordType = suggested
	return view
}

// dnsConfig is environment configuration, not domain state: it is served
// even before any domain is registered so setup instructions can render.
func (s *Service) dnsConfig() *gen.DomainDNSConfig {
	return &gen.DomainDNSConfig{
		CnameTarget: conv.PtrEmpty(s.expectedTargetCNAME),
		ARecords:    FormatARecords(s.expectedARecords),
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) GetDomain(ctx context.Context, payload *gen.GetDomainPayload) (res *gen.CustomDomain, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	repo := repo.New(s.db)

	domain, err := repo.GetCustomDomainByOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "no custom domain found for organization").LogError(ctx, s.logger)
	}

	isUpdating := false
	if workflowInfo, _ := s.temporalClient.GetWorkflowInfo(ctx, authCtx.ActiveOrganizationID, domain.Domain, domain.ID); workflowInfo != nil {
		isUpdating = workflowInfo.GetWorkflowExecutionInfo().GetStatus() == enums.WORKFLOW_EXECUTION_STATUS_RUNNING
	}

	route, err := repo.GetCustomDomainRouteConfig(ctx, domain.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load custom domain route").LogError(ctx, s.logger)
	}

	return s.domainView(domain, isUpdating, route.RootMcpEndpointID), nil
}

func (s *Service) ListDomains(ctx context.Context, _ *gen.ListDomainsPayload) (*gen.ListCustomDomainsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	repo := repo.New(s.db)

	domain, err := repo.GetCustomDomainByOrganization(ctx, authCtx.ActiveOrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return &gen.ListCustomDomainsResult{Domains: []*gen.CustomDomain{}, DNSConfig: s.dnsConfig()}, nil
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get custom domain for organization").LogError(ctx, s.logger)
	}

	isUpdating := false
	if workflowInfo, _ := s.temporalClient.GetWorkflowInfo(ctx, authCtx.ActiveOrganizationID, domain.Domain, domain.ID); workflowInfo != nil {
		isUpdating = workflowInfo.GetWorkflowExecutionInfo().GetStatus() == enums.WORKFLOW_EXECUTION_STATUS_RUNNING
	}

	route, err := repo.GetCustomDomainRouteConfig(ctx, domain.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load custom domain route").LogError(ctx, s.logger)
	}

	return &gen.ListCustomDomainsResult{
		Domains:   []*gen.CustomDomain{s.domainView(domain, isUpdating, route.RootMcpEndpointID)},
		DNSConfig: s.dnsConfig(),
	}, nil
}

// CreateDomain synchronously creates the pending custom-domain row (or finds
// the caller's existing one on reverify), then starts — or wakes — the
// long-lived verification workflow. The row is created unverified: only the
// verification workflow's TXT proof can set verified, and reconciliation only
// activates verified domains.
func (s *Service) CreateDomain(ctx context.Context, payload *gen.CreateDomainPayload) (res *gen.CustomDomain, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	if !canCreateCustomDomain(authCtx.AccountType) {
		return nil, oops.E(oops.CodeUnauthorized, err, "custom domain registration is not supported for free account").LogError(ctx, s.logger)
	}

	domainName := NormalizeDomainName(payload.Domain)
	if err := ValidateDomainName(domainName); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%s", err.Error()).LogError(ctx, s.logger)
	}

	ipAllowlist := payload.IPAllowlist
	if ipAllowlist == nil {
		ipAllowlist = []string{}
	}
	if err := validateIPAllowlist(ipAllowlist); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid ip_allowlist entry").LogError(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to access custom domains").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	repository := repo.New(dbtx)
	domain, err := repository.GetCustomDomainByDomain(ctx, domainName)
	switch {
	case err == nil:
		if domain.OrganizationID != authCtx.ActiveOrganizationID {
			return nil, oops.E(oops.CodeConflict, errors.New("domain is registered to another organization"), "domain is already registered").LogError(ctx, s.logger)
		}
	case errors.Is(err, pgx.ErrNoRows):
		domain, err = repository.CreateCustomDomain(ctx, repo.CreateCustomDomainParams{
			OrganizationID:  authCtx.ActiveOrganizationID,
			Domain:          domainName,
			IngressName:     conv.PtrToPGText(nil),
			CertSecretName:  conv.PtrToPGText(nil),
			ProvisionerKind: string(k8s.ProvisionerKindIngress),
			IpAllowlist:     ipAllowlist,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error creating custom domain").LogError(ctx, s.logger)
		}

		if err := s.audit.LogCustomDomainCreate(ctx, dbtx, audit.LogCustomDomainCreateEvent{
			OrganizationID:   authCtx.ActiveOrganizationID,
			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName: authCtx.Email,
			ActorSlug:        nil,
			CustomDomainURN:  urn.NewCustomDomain(domain.ID),
			DomainName:       domain.Domain,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to create custom domain creation audit log").LogError(ctx, s.logger)
		}
	default:
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get custom domain").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to save custom domain").LogError(ctx, s.logger)
	}

	// Stop any legacy hostname-scoped run (started before workflows were
	// row-scoped, or left behind by a deleted predecessor row) so it cannot
	// poll in parallel with — or act on behalf of — the row-scoped workflow
	// started below.
	if err := s.temporalClient.TerminateCustomDomainRegistration(ctx, authCtx.ActiveOrganizationID, domain.Domain, uuid.Nil, "superseded by row-scoped registration"); err != nil {
		s.logger.WarnContext(ctx, "failed to terminate stale custom domain registration workflow", attr.SlogError(err), attr.SlogURLDomain(domain.Domain))
	}

	_, err = s.temporalClient.ExecuteCustomDomainRegistration(
		ctx,
		authCtx.ActiveOrganizationID,
		domain.Domain,
		domain.ID,
		urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		authCtx.Email,
		k8s.ProvisionerKindIngress,
		ipAllowlist,
	)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error executing custom domain registration").LogError(ctx, s.logger)
	}

	// A reverify of an existing domain keeps its root mapping; the response
	// must not present it as unmapped.
	rootMcpEndpointID := uuid.Nil
	if route, err := repo.New(s.db).GetCustomDomainRouteConfig(ctx, domain.ID); err == nil {
		rootMcpEndpointID = route.RootMcpEndpointID
	} else {
		s.logger.WarnContext(ctx, "failed to load root endpoint for registration response", attr.SlogError(err), attr.SlogURLDomain(domain.Domain))
	}

	return s.domainView(domain, true, rootMcpEndpointID), nil
}

func canCreateCustomDomain(accountType string) bool {
	switch billing.Tier(accountType) {
	case billing.TierPro, billing.TierPayg, billing.TierEnterprise:
		return true
	default:
		return false
	}
}

func (s *Service) UpdateDomain(ctx context.Context, payload *gen.UpdateDomainPayload) (res *gen.CustomDomain, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	if payload.IPAllowlist == nil && payload.OpenaiAppsChallengeToken == nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "provide at least one custom domain setting to update").LogError(ctx, s.logger)
	}
	if payload.IPAllowlist != nil {
		if err := validateIPAllowlist(payload.IPAllowlist); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid ip_allowlist entry").LogError(ctx, s.logger)
		}
	}
	if err := validateOpenAIAppsChallengeToken(payload.OpenaiAppsChallengeToken); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid openai_apps_challenge_token").LogError(ctx, s.logger)
	}

	ipAllowlist := payload.IPAllowlist
	if ipAllowlist == nil {
		ipAllowlist = []string{}
	}
	challengeToken := conv.PtrToPGTextEmpty(payload.OpenaiAppsChallengeToken)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin custom domain update").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	repository := repo.New(dbtx)
	domain, err := repository.LockCustomDomainByOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "no custom domain found for organization").LogError(ctx, s.logger)
	}
	routeBefore, err := repository.GetCustomDomainRouteConfig(ctx, domain.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load custom domain route before update").LogError(ctx, s.logger)
	}
	beforeView := mv.BuildCustomDomainView(domain, false, routeBefore.RootMcpEndpointID)

	domain, err = repository.UpdateCustomDomainSettings(ctx, repo.UpdateCustomDomainSettingsParams{
		UpdateIpAllowlist:              payload.IPAllowlist != nil,
		IpAllowlist:                    ipAllowlist,
		UpdateOpenaiAppsChallengeToken: payload.OpenaiAppsChallengeToken != nil,
		OpenaiAppsChallengeToken:       challengeToken,
		OrganizationID:                 authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "update custom domain settings").LogError(ctx, s.logger)
	}
	afterView := mv.BuildCustomDomainView(domain, false, routeBefore.RootMcpEndpointID)

	if err := s.audit.LogCustomDomainUpdate(ctx, dbtx, audit.LogCustomDomainUpdateEvent{
		OrganizationID:             authCtx.ActiveOrganizationID,
		Actor:                      urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:           authCtx.Email,
		ActorSlug:                  nil,
		CustomDomainURN:            urn.NewCustomDomain(domain.ID),
		DomainName:                 domain.Domain,
		CustomDomainSnapshotBefore: beforeView,
		CustomDomainSnapshotAfter:  afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log custom domain update").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit custom domain update").LogError(ctx, s.logger)
	}
	if payload.IPAllowlist != nil || payload.OpenaiAppsChallengeToken != nil {
		if err := s.reconcileCustomDomain(ctx, domain.ID); err != nil {
			return nil, err
		}
	}

	return s.domainView(domain, false, routeBefore.RootMcpEndpointID), nil
}

func validateOpenAIAppsChallengeToken(token *string) error {
	if token == nil || *token == "" {
		return nil
	}
	if utf8.RuneCountInString(*token) > 256 {
		return errors.New("token must be at most 256 characters")
	}
	if strings.ContainsAny(*token, "\r\n") {
		return errors.New("token must be a single line")
	}
	return nil
}

func (s *Service) SetRootMcpEndpoint(ctx context.Context, payload *gen.SetRootMcpEndpointPayload) (*gen.CustomDomain, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	customDomainID, err := uuid.Parse(payload.CustomDomainID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid custom_domain_id").LogError(ctx, s.logger)
	}
	if payload.McpEndpointID != nil && payload.McpServerID != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "provide either mcp_endpoint_id or mcp_server_id, not both").LogError(ctx, s.logger)
	}
	var targetID uuid.UUID
	targetValid := payload.McpEndpointID != nil
	if targetValid {
		targetID, err = uuid.Parse(*payload.McpEndpointID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp_endpoint_id").LogError(ctx, s.logger)
		}
	}
	var rootServerID uuid.UUID
	if payload.McpServerID != nil {
		rootServerID, err = uuid.Parse(*payload.McpServerID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp_server_id").LogError(ctx, s.logger)
		}
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin root endpoint update").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	repository := repo.New(dbtx)
	domain, err := repository.LockCustomDomainByIDAndOrganization(ctx, repo.LockCustomDomainByIDAndOrganizationParams{
		ID:             customDomainID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, err, "custom domain not found").LogError(ctx, s.logger)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock custom domain for root endpoint update").LogError(ctx, s.logger)
	}
	beforeRoute, err := repository.GetCustomDomainRouteConfig(ctx, domain.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load current root endpoint").LogError(ctx, s.logger)
	}
	beforeView := mv.BuildCustomDomainView(domain, false, beforeRoute.RootMcpEndpointID)

	if rootServerID != uuid.Nil {
		// Custom domains are org-scoped, so the org:admin gate above
		// authorizes creating the endpoint in the server's project, mirroring
		// DeleteDomain's cross-project cascade.
		server, err := repository.GetEligibleRootMcpServerForOrganization(ctx, repo.GetEligibleRootMcpServerForOrganizationParams{
			McpServerID:    rootServerID,
			OrganizationID: authCtx.ActiveOrganizationID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeInvalid, err, "mcp server is not eligible for this custom domain").LogError(ctx, s.logger)
		}
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "validate root mcp server").LogError(ctx, s.logger)
		}

		endpoint, err := repository.GetMcpEndpointByCustomDomainAndServer(ctx, repo.GetMcpEndpointByCustomDomainAndServerParams{
			CustomDomainID: domain.ID,
			McpServerID:    rootServerID,
		})
		switch {
		case err == nil:
			targetID = endpoint.ID
		case errors.Is(err, pgx.ErrNoRows):
			if !server.Slug.Valid || server.Slug.String == "" {
				return nil, oops.E(oops.CodeBadRequest, nil, "mcp server has no slug to name its domain endpoint; attach an endpoint to the domain first").LogError(ctx, s.logger)
			}
			// Direct repo calls: the mcpendpoints service package would be an
			// import cycle through background.
			if err := mcpendpointsrepo.New(dbtx).LockSlugScope(ctx, mcpendpointsrepo.LockSlugScopeParams{
				CustomDomainID: uuid.NullUUID{UUID: domain.ID, Valid: true},
				Slug:           server.Slug.String,
			}); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "lock mcp endpoint slug scope").LogError(ctx, s.logger)
			}
			available, err := mcpendpointsrepo.New(dbtx).CheckUnifiedSlugAvailability(ctx, mcpendpointsrepo.CheckUnifiedSlugAvailabilityParams{
				Slug:               server.Slug.String,
				CustomDomainID:     uuid.NullUUID{UUID: domain.ID, Valid: true},
				OrganizationID:     authCtx.ActiveOrganizationID,
				ExcludeToolsetID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
				ExcludeMcpServerID: uuid.NullUUID{UUID: rootServerID, Valid: true},
				SkipDomainCheck:    false,
			})
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "check mcp endpoint slug availability").LogError(ctx, s.logger)
			}
			if !available.Bool {
				return nil, oops.E(oops.CodeConflict, nil, "an endpoint named %s already exists on this domain", server.Slug.String).LogError(ctx, s.logger)
			}
			// Deliberately narrower than the mcpendpoints creation flow: no
			// default-plugin attachment or marketplace publish — those are
			// project-scoped concerns, and mapping a domain root should not
			// publish the server anywhere.
			created, err := mcpendpointsrepo.New(dbtx).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
				ProjectID:       server.ProjectID,
				CustomDomainID:  uuid.NullUUID{UUID: domain.ID, Valid: true},
				McpServerID:     uuid.NullUUID{UUID: rootServerID, Valid: true},
				MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
				Slug:            server.Slug.String,
			})
			if err != nil {
				var pgErr *pgconn.PgError
				if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
					return nil, oops.E(oops.CodeConflict, err, "an endpoint named %s already exists on this domain", server.Slug.String).LogError(ctx, s.logger)
				}
				return nil, oops.E(oops.CodeUnexpected, err, "create domain endpoint for root mcp server").LogError(ctx, s.logger)
			}
			if err := s.audit.LogMcpEndpointCreate(ctx, dbtx, audit.LogMcpEndpointCreateEvent{
				OrganizationID:   authCtx.ActiveOrganizationID,
				ProjectID:        server.ProjectID,
				Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
				ActorDisplayName: authCtx.Email,
				ActorSlug:        nil,
				McpEndpointURN:   urn.NewMcpEndpoint(created.ID),
				Slug:             created.Slug,
			}); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "log domain endpoint creation").LogError(ctx, s.logger)
			}
			targetID = created.ID
		default:
			return nil, oops.E(oops.CodeUnexpected, err, "find domain endpoint for root mcp server").LogError(ctx, s.logger)
		}
		targetValid = true
	}

	if _, err := repository.LockRootMcpEndpointSelection(ctx, repo.LockRootMcpEndpointSelectionParams{
		CustomDomainID: domain.ID,
		McpEndpointID:  uuid.NullUUID{UUID: targetID, Valid: targetValid},
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock root endpoint selection").LogError(ctx, s.logger)
	}

	if targetValid {
		if _, err := repository.GetEligibleRootMcpEndpoint(ctx, repo.GetEligibleRootMcpEndpointParams{
			McpEndpointID:  targetID,
			CustomDomainID: domain.ID,
			OrganizationID: authCtx.ActiveOrganizationID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeInvalid, err, "mcp endpoint is not eligible for this custom domain").LogError(ctx, s.logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "validate root mcp endpoint").LogError(ctx, s.logger)
		}
	}

	if err := repository.ClearRootMcpEndpoint(ctx, domain.ID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "clear existing root endpoint").LogError(ctx, s.logger)
	}
	if targetValid {
		if err := repository.SetRootMcpEndpoint(ctx, repo.SetRootMcpEndpointParams{
			McpEndpointID:  targetID,
			CustomDomainID: domain.ID,
		}); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
				return nil, oops.E(oops.CodeConflict, err, "another root endpoint selection is being updated; retry the request").LogError(ctx, s.logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "set root mcp endpoint").LogError(ctx, s.logger)
		}
	}

	afterView := mv.BuildCustomDomainView(domain, false, targetID)
	if err := s.audit.LogCustomDomainUpdate(ctx, dbtx, audit.LogCustomDomainUpdateEvent{
		OrganizationID:             authCtx.ActiveOrganizationID,
		Actor:                      urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:           authCtx.Email,
		ActorSlug:                  nil,
		CustomDomainURN:            urn.NewCustomDomain(domain.ID),
		DomainName:                 domain.Domain,
		CustomDomainSnapshotBefore: beforeView,
		CustomDomainSnapshotAfter:  afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log root endpoint update").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit root endpoint update").LogError(ctx, s.logger)
	}
	if err := s.reconcileCustomDomain(ctx, domain.ID); err != nil {
		return nil, err
	}
	return s.domainView(domain, false, targetID), nil
}

func (s *Service) reconcileCustomDomain(ctx context.Context, customDomainID uuid.UUID) error {
	run, err := s.temporalClient.ExecuteCustomDomainReconcile(ctx, customDomainID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to start custom domain reconciliation").LogError(ctx, s.logger)
	}
	if err := run.Get(ctx, nil); err != nil {
		return oops.E(oops.CodeUnexpected, err, "custom domain reconciliation failed").LogError(ctx, s.logger)
	}
	return nil
}

func (s *Service) CheckHealth(ctx context.Context, _ *gen.CheckHealthPayload) (*gen.CustomDomain, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	repository := repo.New(s.db)
	domain, err := repository.GetCustomDomainByOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "no custom domain found for organization").LogError(ctx, s.logger)
	}
	if !domain.Activated {
		return nil, oops.E(oops.CodeBadRequest, errors.New("custom domain is not activated"), "custom domain is not activated yet; health checks run after activation").LogError(ctx, s.logger)
	}

	run, err := s.temporalClient.ExecuteCustomDomainHealthCheck(ctx, authCtx.ActiveOrganizationID, domain.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to start custom domain health check").LogError(ctx, s.logger)
	}
	if err := run.Get(ctx, nil); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "custom domain health check failed").LogError(ctx, s.logger)
	}

	domain, err = repository.GetCustomDomainByIDAndOrganization(ctx, repo.GetCustomDomainByIDAndOrganizationParams{
		ID:             domain.ID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get custom domain after health check").LogError(ctx, s.logger)
	}
	route, err := repository.GetCustomDomainRouteConfig(ctx, domain.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load custom domain route after health check").LogError(ctx, s.logger)
	}
	return s.domainView(domain, false, route.RootMcpEndpointID), nil
}

func (s *Service) DeleteDomain(ctx context.Context, _ *gen.DeleteDomainPayload) (err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}

	repository := repo.New(s.db)
	domain, err := repository.GetCustomDomainByOrganization(ctx, authCtx.ActiveOrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		pending, pendingErr := repository.GetPendingDeletedCustomDomainByOrganization(ctx, authCtx.ActiveOrganizationID)
		if errors.Is(pendingErr, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "no custom domain found for organization").LogError(ctx, s.logger)
		}
		if pendingErr != nil {
			return oops.E(oops.CodeUnexpected, pendingErr, "find custom domain cleanup pending retry").LogError(ctx, s.logger)
		}
		if err := s.reconcileCustomDomain(ctx, pending.ID); err != nil {
			return err
		}
		return nil
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get custom domain for deletion").LogError(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to access custom domains").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	// Lock+reload inside tx: stale pre-tx read must not checkpoint or delete a row that changed underneath us (tombstone repopulation / successor deletion).
	domain, err = repo.New(dbtx).LockCustomDomainByOrganization(ctx, authCtx.ActiveOrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Concurrent delete won; its reconcile signal owns cleanup.
		return nil
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock custom domain for deletion").LogError(ctx, s.logger)
	}

	if s.networkIngressPins == nil {
		return oops.E(oops.CodeUnexpected, nil, "network ingress pin checker is unavailable").LogError(ctx, s.logger)
	}
	pinned, err := s.networkIngressPins(ctx, dbtx, authCtx.ActiveOrganizationID, domain.ID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "check custom domain network ingress pin").LogError(ctx, s.logger)
	}
	if pinned {
		return oops.E(oops.CodeConflict, nil, "custom domain is pinned by an active network ingress")
	}

	// The mcp_endpoints.custom_domain_id FK has ON DELETE SET NULL, but that
	// only fires for hard deletes. Soft-delete the child endpoints explicitly
	// so they don't outlive the tombstoned custom domain. The cascade spans
	// every project under the owning org — custom domains are org-scoped —
	// so the org:admin gate above authorizes the entire fan-out.
	deletedEndpoints, err := mcpendpointsrepo.New(dbtx).SoftDeleteMCPEndpointsByCustomDomainID(ctx, domain.ID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete child mcp endpoints").LogError(ctx, s.logger)
	}

	for _, endpoint := range deletedEndpoints {
		if err := s.audit.LogMcpEndpointDelete(ctx, dbtx, audit.LogMcpEndpointDeleteEvent{
			OrganizationID:   authCtx.ActiveOrganizationID,
			ProjectID:        endpoint.ProjectID,
			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName: authCtx.Email,
			ActorSlug:        nil,
			McpEndpointURN:   urn.NewMcpEndpoint(endpoint.ID),
			Slug:             endpoint.Slug,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "log mcp endpoint deletion").LogError(ctx, s.logger)
		}
	}

	// Checkpoint derived identity: tombstone without names invisible to GetPendingDeletedCustomDomainByOrganization, so partial-Apply leak unrecoverable. Unconditional — COALESCE fills only the missing fields (e.g. legacy rows with a name but no secret).
	if kind := domain.ProvisionerKind; kind == "" || kind == string(k8s.ProvisionerKindIngress) {
		resourceName, nameErr := k8s.SanitizeDomainForK8sName(domain.Domain)
		if nameErr != nil {
			s.logger.WarnContext(ctx, "skipping custom domain resource identity checkpoint", attr.SlogError(nameErr), attr.SlogURLDomain(domain.Domain))
		} else if rows, err := repo.New(dbtx).EnsureCustomDomainResourceNames(ctx, repo.EnsureCustomDomainResourceNamesParams{
			IngressName:    conv.ToPGText(resourceName),
			CertSecretName: conv.ToPGText(k8s.TLSSecretNameForDomain(domain.Domain)),
			ID:             domain.ID,
			OrganizationID: authCtx.ActiveOrganizationID,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "checkpoint custom domain resource identity").LogError(ctx, s.logger)
		} else if rows != 1 {
			return oops.E(oops.CodeUnexpected, fmt.Errorf("expected 1 row, updated %d", rows), "checkpoint custom domain resource identity").LogError(ctx, s.logger)
		}
	}

	if err := repo.New(dbtx).DeleteCustomDomain(ctx, authCtx.ActiveOrganizationID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to delete custom domain").LogError(ctx, s.logger)
	}

	if err := s.audit.LogCustomDomainDelete(ctx, dbtx, audit.LogCustomDomainDeleteEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,

		CustomDomainURN: urn.NewCustomDomain(domain.ID),
		DomainName:      domain.Domain,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to create custom domain deletion audit log").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to commit custom domain deletion").LogError(ctx, s.logger)
	}

	// A registration workflow can outlive the row by hours while it waits for
	// DNS; stop it so it does not keep polling a deleted identity.
	if err := s.temporalClient.TerminateCustomDomainRegistration(ctx, authCtx.ActiveOrganizationID, domain.Domain, domain.ID, "custom domain deleted"); err != nil {
		s.logger.WarnContext(ctx, "failed to terminate custom domain registration workflow", attr.SlogError(err), attr.SlogURLDomain(domain.Domain))
	}

	if err := s.reconcileCustomDomain(ctx, domain.ID); err != nil {
		return err
	}

	s.logger.InfoContext(ctx, "custom domain deleted",
		attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		attr.SlogURLDomain(domain.Domain),
	)

	return nil
}

// ListRootMcpServers lists every MCP server in the organization that can be
// mapped to the custom domain root, including servers with no endpoint on the
// domain yet. Works before any domain is registered (attachment info is
// simply absent).
func (s *Service) ListRootMcpServers(ctx context.Context, _ *gen.ListRootMcpServersPayload) (*gen.ListRootMcpServersResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	repository := repo.New(s.db)
	domainID := uuid.Nil
	domain, err := repository.GetCustomDomainByOrganization(ctx, authCtx.ActiveOrganizationID)
	switch {
	case err == nil:
		domainID = domain.ID
	case errors.Is(err, pgx.ErrNoRows):
	default:
		return nil, oops.E(oops.CodeUnexpected, err, "get custom domain for root server listing").LogError(ctx, s.logger)
	}

	rows, err := repository.ListEligibleRootMcpServersForOrganization(ctx, repo.ListEligibleRootMcpServersForOrganizationParams{
		CustomDomainID: domainID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list eligible root mcp servers").LogError(ctx, s.logger)
	}

	options := make([]*gen.RootMcpServerOption, 0, len(rows))
	for _, row := range rows {
		options = append(options, &gen.RootMcpServerOption{
			McpServerID:          row.ID.String(),
			Name:                 conv.FromPGText[string](row.Name),
			Slug:                 conv.FromPGText[string](row.Slug),
			ProjectID:            row.ProjectID.String(),
			ProjectName:          row.ProjectName,
			AttachedEndpointID:   conv.PtrEmpty(conv.Ternary(row.AttachedEndpointID != uuid.Nil, row.AttachedEndpointID.String(), "")),
			AttachedEndpointSlug: conv.PtrEmpty(row.EndpointSlug),
			IsDomainRoot:         row.IsDomainRoot,
		})
	}
	return &gen.ListRootMcpServersResult{McpServers: options}, nil
}

func (s *Service) ListMcpEndpoints(ctx context.Context, _ *gen.ListMcpEndpointsPayload) (*gen.ListCustomDomainMcpEndpointsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	domain, err := repo.New(s.db).GetCustomDomainByOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "no custom domain found for organization").LogError(ctx, s.logger)
	}

	rows, err := mcpendpointsrepo.New(s.db).ListMCPEndpointsByCustomDomainID(ctx, domain.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list mcp endpoints by custom domain").LogError(ctx, s.logger)
	}

	return &gen.ListCustomDomainMcpEndpointsResult{McpEndpoints: mv.BuildCustomDomainMcpEndpointListView(rows)}, nil
}
