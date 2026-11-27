package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	environmentsrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/mv"
	organizations_repo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// ProjectGatewayEvaluatorConfig carries the dependencies needed to run
// Project Gateway admission and member resolution in processes (such as the
// Temporal worker) that do not host the full MCP HTTP service.
type ProjectGatewayEvaluatorConfig struct {
	Logger                    *slog.Logger
	MeterProvider             metric.MeterProvider
	DB                        *pgxpool.Pool
	Environment               toolconfig.EnvironmentLoader
	Encryption                *encryption.Client
	Cache                     cache.Cache
	Authz                     *authz.Engine
	Features                  feature.Provider
	PrincipalCredentials      *principalcredential.Issuer
	AssistantTokens           *assistanttokens.Manager
	ServerURL                 *url.URL
	AuthenticationHostBaseURL string
	PlatformHosts             map[string]string
	PlatformExtras            []platformtools.ExternalTool
}

// ProjectGatewayEvaluator evaluates Project Gateway eligibility using the exact
// same admission and member resolution methods as *Service.
type ProjectGatewayEvaluator struct {
	service *Service
}

// NewProjectGatewayEvaluator builds a lightweight evaluator backed by the same
// Project Gateway admission and member resolution code path as *Service.
func NewProjectGatewayEvaluator(cfg ProjectGatewayEvaluatorConfig) *ProjectGatewayEvaluator {
	platformToolsets := make(map[string]platformtools.Toolset, len(implicitGatewayPlatformSlugs))
	for _, slug := range implicitGatewayPlatformSlugs {
		platformToolsets[slug] = platformtools.Toolset{Slug: slug, Tools: nil, ActingContext: nil}
	}
	meter := cfg.MeterProvider.Meter("github.com/speakeasy-api/gram/server/internal/mcp")
	return &ProjectGatewayEvaluator{
		service: &Service{ //nolint:exhaustruct_v5 // lightweight evaluator only invokes gateway admission and derived member resolution
			logger:                    cfg.Logger,
			db:                        cfg.DB,
			env:                       cfg.Environment,
			enc:                       cfg.Encryption,
			authz:                     cfg.Authz,
			features:                  cfg.Features,
			principalCredentials:      cfg.PrincipalCredentials,
			assistantTokens:           cfg.AssistantTokens,
			serverURL:                 cfg.ServerURL,
			authenticationHostBaseURL: cfg.AuthenticationHostBaseURL,
			platformHosts:             cfg.PlatformHosts,
			orgsRepo:                  organizations_repo.New(cfg.DB),
			metrics:                   mcpmetrics.NewMetrics(meter, cfg.Logger),
			identityValidator:         mcpidentity.NewValidatorBoundary(),
			toolsetCache:              cache.NewTypedObjectCache[mv.ToolsetBaseContents](cfg.Logger, cfg.Cache, cache.SuffixNone),
			platformToolsets:          platformToolsets,
			platformExtras:            cfg.PlatformExtras,
		},
	}
}

// EvaluateProjectGatewayForTurnCredential runs the Project Gateway's own
// availability, HTTPS URL, machine-access, turn-credential admission, and
// derived member resolution checks.
func (e *ProjectGatewayEvaluator) EvaluateProjectGatewayForTurnCredential(
	ctx context.Context,
	organizationID string,
	projectID uuid.UUID,
	turnToken string,
	serverURL *url.URL,
) (orggateway.ProjectGatewayEvaluation, error) {
	return e.service.EvaluateProjectGatewayForTurnCredential(ctx, organizationID, projectID, turnToken, serverURL)
}

// EvaluateProjectGatewayForTurnCredential runs the Project Gateway's own
// availability, HTTPS URL, machine-access, turn-credential admission, and
// member resolution path for a single assistant turn credential, so callers
// deciding whether to route a turn to the Project Gateway can never disagree
// with what the gateway will admit or serve.
func (s *Service) EvaluateProjectGatewayForTurnCredential(
	ctx context.Context,
	organizationID string,
	projectID uuid.UUID,
	turnToken string,
	serverURL *url.URL,
) (orggateway.ProjectGatewayEvaluation, error) {
	deny := func(reason string) orggateway.ProjectGatewayEvaluation {
		return orggateway.ProjectGatewayEvaluation{Admitted: false, GatewayURL: "", Reason: reason, Members: nil}
	}
	if serverURL == nil || s.serverURL == nil || organizationID == "" || projectID == uuid.Nil || turnToken == "" || s.principalCredentials == nil {
		return deny("missing_inputs"), nil
	}

	organization, err := s.orgsRepo.GetOrganizationMetadata(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return deny("organization_not_found"), nil
	}
	if err != nil {
		return deny(""), fmt.Errorf("load organization for project gateway eligibility: %w", err)
	}
	project, err := projectsrepo.New(s.db).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{
		ID:             projectID,
		OrganizationID: organizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return deny("project_not_found"), nil
	}
	if err != nil {
		return deny(""), fmt.Errorf("load project for project gateway eligibility: %w", err)
	}

	rawURL := orggateway.ProjectURL(serverURL, organization.Slug, project.Slug)
	parsedURL, err := url.Parse(rawURL)
	if err != nil || parsedURL.Scheme != "https" || parsedURL.Host == "" || parsedURL.User != nil || parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return deny("non_https_gateway_url"), nil
	}

	metaServer, endpoint, unavailable, err := s.organizationGatewayRows(ctx, organization, orggateway.Availability)
	if err != nil {
		return deny(""), err
	}
	if unavailable != orggateway.UnavailabilityNone {
		return deny(string(unavailable)), nil
	}
	metaServer.ID = orggateway.ProjectGatewayID(organization.ID, project.ID)
	metaServer.ProjectID = project.ID
	metaServer.Name = project.Name
	endpoint.ProjectID = project.ID
	endpoint.Slug = organization.Slug + "/" + project.Slug
	resolved := NewResolvedMcpEndpointFromMetaMcpServer(endpoint, metaServer, organization.ID, mcproute.OrganizationGateway)
	if err := s.RequireUserSessionIssuer(ctx, resolved); err != nil {
		if errors.Is(err, errUserSessionIssuerRetired) {
			return deny("issuer_retired"), nil
		}
		return deny(""), err
	}

	target, err := gatewayTargetFromEndpoint(resolved)
	if err != nil {
		return deny(""), err
	}
	machineAccess, err := s.gatewayMachineAccess(ctx, target)
	if err != nil {
		return deny(""), err
	}
	if !machineAccess {
		return deny("machine_access_disabled"), nil
	}

	authedCtx, subject, err := s.authenticateIssuerGatePrincipalCredential(ctx, turnToken, resolved)
	if err != nil {
		if errors.Is(err, errCredentialRejected) || isCredentialDenial(err) {
			return deny("credential_rejected"), nil
		}
		return deny(""), err
	}
	caller, err := gatewayCallerFromContext(authedCtx, *subject)
	if err != nil {
		return deny("invalid_caller"), nil
	}
	admitted, err := target.admits(caller, resolved.ProjectID, machineAccess)
	if err != nil {
		return deny(""), err
	}
	if !admitted {
		return deny("caller_not_admitted"), nil
	}
	if caller.kind == gatewayCallerAgent || caller.kind == gatewayCallerWorkload {
		if _, ok := contextvalues.EffectiveAgentID(authedCtx); !ok {
			return deny("missing_effective_agent"), nil
		}
	}

	authedCtx, members, _, err := s.resolveOrganizationMemberSnapshot(
		authedCtx,
		s.logger,
		mandatorySurfaceGatewayMember,
		organization.ID,
		false,
		uuid.Nil,
		project.ID,
		true,
	)
	if err != nil {
		return deny(""), err
	}

	out := make([]orggateway.ProjectGatewayMember, 0, len(members))
	for _, member := range members {
		envSlug, err := s.projectGatewayMemberEnvironmentSlug(authedCtx, member)
		if err != nil {
			return deny(""), err
		}
		out = append(out, orggateway.ProjectGatewayMember{
			ServerID:            member.serverID,
			PlatformToolsetSlug: member.platformToolsetSlug,
			EnvironmentSlug:     envSlug,
		})
	}
	return orggateway.ProjectGatewayEvaluation{
		Admitted:   true,
		GatewayURL: rawURL,
		Reason:     "",
		Members:    out,
	}, nil
}

// projectGatewayMemberEnvironmentSlug returns the Speakeasy environment slug
// the Project Gateway uses when serving member (mirroring loadMemberToolset
// and derivedMemberAuth).
func (s *Service) projectGatewayMemberEnvironmentSlug(ctx context.Context, member metaMember) (string, error) {
	if member.backend != metaMemberBackendHosted || !member.toolsetID.Valid {
		return "", nil
	}
	if member.environmentID.Valid {
		row, err := environmentsrepo.New(s.db).GetEnvironmentByID(ctx, environmentsrepo.GetEnvironmentByIDParams{
			ID:        member.environmentID.UUID,
			ProjectID: member.projectID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// Dangling environment: fall through to toolset default.
		case err != nil:
			return "", fmt.Errorf("load member environment: %w", err)
		default:
			return row.Slug, nil
		}
	}
	toolset, err := toolsetsrepo.New(s.db).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{
		ID:        member.toolsetID.UUID,
		ProjectID: member.projectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("load member toolset default environment: %w", err)
	}
	return toolset.DefaultEnvironmentSlug.String, nil
}
