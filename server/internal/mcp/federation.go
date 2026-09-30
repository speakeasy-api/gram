// What an external platform must be pointed at to exchange its workload
// identity token for a session on one of an MCP server's addresses.

package mcp

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomains_repo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	mcpendpoints_repo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// FederationNotReadyReason says why a workload token exchange at an endpoint
// cannot succeed, whatever the platform is configured with.
type FederationNotReadyReason string

const (
	// FederationReady is the zero reason: nothing Gram knows of stops the
	// exchange.
	FederationReady FederationNotReadyReason = ""

	// FederationNotPubliclyReachable means the address does not resolve on
	// the public surface: the server is disabled or reachable only through a
	// private network.
	FederationNotPubliclyReachable FederationNotReadyReason = "not_publicly_reachable"

	// FederationNoAuthorizationServer means Gram is not the endpoint's
	// authorization server, because the server is not gated on a Gram user
	// session issuer or is an anonymous public tunnel.
	FederationNoAuthorizationServer FederationNotReadyReason = "no_authorization_server"

	// FederationWorkloadGrantUnavailable means the authorization server does
	// not advertise the clientless jwt-bearer grant for this endpoint.
	FederationWorkloadGrantUnavailable FederationNotReadyReason = "workload_grant_unavailable"

	// FederationAgentRolloutDisabled means the grant is advertised but the
	// organization is outside the agent authorization rollout, which the
	// token endpoint enforces.
	FederationAgentRolloutDisabled FederationNotReadyReason = "agent_rollout_disabled"
)

// FederationEndpoint is one address of an MCP server as a federating platform
// sees it. Issuer, TokenEndpoint and GrantTypesSupported are the values the
// endpoint's RFC 8414 document serves; they are empty when Gram is not the
// endpoint's authorization server.
type FederationEndpoint struct {
	// ResourceURL is the MCP server URL: the resource a session is minted
	// for and the audience of the token the platform receives.
	ResourceURL string

	// Issuer is the authorization server's issuer identifier.
	Issuer string

	// TokenEndpoint is where the platform sends its assertion.
	TokenEndpoint string

	// OnAuthenticationHost reports whether the token endpoint is on Gram's
	// dedicated authentication host rather than the resource's own host.
	OnAuthenticationHost bool

	// GrantTypesSupported is grant_types_supported as the document lists it.
	GrantTypesSupported []string

	// WorkloadGrantAdvertised reports whether jwt-bearer is listed because
	// the clientless workload assertion grant is available here.
	WorkloadGrantAdvertised bool

	// NotReady is why an exchange here cannot succeed, or FederationReady.
	NotReady FederationNotReadyReason
}

// FederationEndpoints resolves every address of server that a platform
// outside the organization could federate with: its endpoint addresses,
// platform-host addresses first, then the addresses a toolset-backed server
// still answers on through its toolset's MCP slug. Each is resolved as a
// public request for it would be, and its values come from the same derivation
// as the endpoint's discovery document. The caller is responsible for
// authorizing the read of server.
//
// A domain-root address is reported at its slug path: the custom domain's
// ingress forwards the root and its discovery documents to that path, so the
// served resource, issuer and token endpoint all name it.
func (s *Service) FederationEndpoints(ctx context.Context, organizationID string, server *mcpservers_repo.McpServer) ([]FederationEndpoint, error) {
	logger := s.logger.With(attr.SlogMcpServerID(server.ID.String()), attr.SlogOrganizationID(organizationID))

	rows, err := mcpendpoints_repo.New(s.db).ListAddressableMCPEndpointsByMCPServerID(ctx, mcpendpoints_repo.ListAddressableMCPEndpointsByMCPServerIDParams{
		OrganizationID: organizationID,
		ProjectID:      server.ProjectID,
		McpServerID:    server.ID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list mcp server endpoints").LogError(ctx, logger)
	}
	slices.SortFunc(rows, func(a, b mcpendpoints_repo.ListAddressableMCPEndpointsByMCPServerIDRow) int {
		if a.McpEndpoint.CustomDomainID.Valid != b.McpEndpoint.CustomDomainID.Valid {
			if a.McpEndpoint.CustomDomainID.Valid {
				return 1
			}
			return -1
		}
		return cmp.Or(
			a.McpEndpoint.CreatedAt.Time.Compare(b.McpEndpoint.CreatedAt.Time),
			strings.Compare(a.McpEndpoint.ID.String(), b.McpEndpoint.ID.String()),
		)
	})

	endpoints := make([]FederationEndpoint, 0, len(rows))
	for _, row := range rows {
		address := federationAddress{
			slug:           row.McpEndpoint.Slug,
			namespace:      mcpendpoints.NamespacePlatform,
			customDomainID: row.McpEndpoint.CustomDomainID,
			baseURL:        s.platformBaseURL(),
		}
		if row.McpEndpoint.CustomDomainID.Valid {
			address.namespace = mcpendpoints.NamespaceCustomDomain
			address.baseURL = "https://" + row.CustomDomain.String
		}
		endpoint, err := s.federationEndpoint(ctx, logger, organizationID, server.ID, address)
		if err != nil {
			return nil, err
		}
		endpoints = append(endpoints, endpoint)
	}

	legacy, err := s.legacyFederationEndpoints(ctx, logger, organizationID, server)
	if err != nil {
		return nil, err
	}
	return append(endpoints, legacy...), nil
}

// federationAddress is one address an MCP request can arrive at: a slug in
// the platform namespace or on a custom domain.
type federationAddress struct {
	// slug is the address's path segment under /mcp.
	slug string

	// namespace is where the slug is looked up.
	namespace mcpendpoints.NamespaceKind

	// customDomainID is the custom domain the address is on, or invalid for
	// the platform host.
	customDomainID uuid.NullUUID

	// baseURL is the origin a request for the address arrives at.
	baseURL string
}

func (s *Service) platformBaseURL() string {
	return strings.TrimSuffix(s.serverURL.String(), "/")
}

func newFederationEndpoint(resourceURL string) FederationEndpoint {
	return FederationEndpoint{
		ResourceURL:             resourceURL,
		Issuer:                  "",
		TokenEndpoint:           "",
		OnAuthenticationHost:    false,
		GrantTypesSupported:     []string{},
		WorkloadGrantAdvertised: false,
		NotReady:                FederationReady,
	}
}

func (s *Service) federationEndpoint(
	ctx context.Context,
	logger *slog.Logger,
	organizationID string,
	serverID uuid.UUID,
	address federationAddress,
) (FederationEndpoint, error) {
	resourceURL, err := url.JoinPath(address.baseURL, "mcp", address.slug)
	if err != nil {
		return FederationEndpoint{}, oops.E(oops.CodeUnexpected, err, "build resource URL").LogError(ctx, logger)
	}
	federation := newFederationEndpoint(resourceURL)

	resolved, err := mcpendpoints.Resolve(ctx, s.db, logger, mcpendpoints.ResolutionInput{
		Slug:                 address.slug,
		NamespaceKind:        address.namespace,
		CustomDomainID:       address.customDomainID,
		ExpectedOrganization: organizationID,
		Surface:              networkaccess.SurfacePublic,
	})
	if err != nil {
		return FederationEndpoint{}, fmt.Errorf("resolve mcp endpoint: %w", err)
	}
	if !resolved.Found || !resolved.Allowed || resolved.Server == nil || resolved.Server.ID != serverID {
		federation.NotReady = FederationNotPubliclyReachable
		return federation, nil
	}
	if !resolved.Server.UserSessionIssuerID.Valid || isTunneledPublic(resolved.Server) {
		federation.NotReady = FederationNoAuthorizationServer
		return federation, nil
	}

	endpoint, err := s.BuildResolvedMcpEndpointForServer(ctx, logger, resolved.Endpoint, resolved.Server, "mcp")
	if err != nil {
		return FederationEndpoint{}, err
	}
	if err := s.discoverFederation(ctx, logger, &federation, endpoint, address.baseURL); err != nil {
		return FederationEndpoint{}, err
	}
	return federation, nil
}

// legacyFederationEndpoints resolves the addresses a toolset-backed server
// answers on through its toolset's MCP slug. The serving path falls back to
// that slug when no endpoint row claims it: on the toolset's custom domain
// when it has one, and on the platform host, where a slug without a
// platform-host toolset also reaches a custom-domain toolset.
func (s *Service) legacyFederationEndpoints(ctx context.Context, logger *slog.Logger, organizationID string, server *mcpservers_repo.McpServer) ([]FederationEndpoint, error) {
	if !server.ToolsetID.Valid {
		return nil, nil
	}
	toolset, err := s.toolsetsRepo.GetToolsetByIDAndProject(ctx, toolsets_repo.GetToolsetByIDAndProjectParams{
		ID:        server.ToolsetID.UUID,
		ProjectID: server.ProjectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load mcp server toolset").LogError(ctx, logger)
	}
	slug := conv.FromPGTextOrEmpty[string](toolset.McpSlug)
	if slug == "" || !toolset.McpEnabled {
		return nil, nil
	}

	platform := federationAddress{
		slug:           slug,
		namespace:      mcpendpoints.NamespacePlatform,
		customDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		baseURL:        s.platformBaseURL(),
	}
	candidates := []federationAddress{platform}
	home := platform
	if toolset.CustomDomainID.Valid {
		domain, err := customdomains_repo.New(s.db).GetCustomDomainByIDAndOrganization(ctx, customdomains_repo.GetCustomDomainByIDAndOrganizationParams{
			ID:             toolset.CustomDomainID.UUID,
			OrganizationID: organizationID,
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeUnexpected, err, "load toolset custom domain").LogError(ctx, logger)
		}
		if err == nil {
			home = federationAddress{
				slug:           slug,
				namespace:      mcpendpoints.NamespaceCustomDomain,
				customDomainID: toolset.CustomDomainID,
				baseURL:        "https://" + domain.Domain,
			}
			candidates = append(candidates, home)
		}
	}

	endpoints := make([]FederationEndpoint, 0, len(candidates))
	for _, address := range candidates {
		endpoint, err := s.legacyFederationEndpoint(ctx, logger, organizationID, &toolset, address, address == home)
		if err != nil {
			return nil, err
		}
		if endpoint != nil {
			endpoints = append(endpoints, *endpoint)
		}
	}
	return endpoints, nil
}

// legacyFederationEndpoint resolves toolset's slug at address as the serving
// path's toolset fallback does. It returns nil when the address is not the
// toolset's: an endpoint row claims the slug there, or, away from the
// toolset's home address, the slug reaches another toolset or none. At the
// home address a toolset the fallback refuses is reported as not publicly
// reachable.
func (s *Service) legacyFederationEndpoint(
	ctx context.Context,
	logger *slog.Logger,
	organizationID string,
	toolset *toolsets_repo.Toolset,
	address federationAddress,
	home bool,
) (*FederationEndpoint, error) {
	claimed, err := mcpendpoints.Resolve(ctx, s.db, logger, mcpendpoints.ResolutionInput{
		Slug:                 address.slug,
		NamespaceKind:        address.namespace,
		CustomDomainID:       address.customDomainID,
		ExpectedOrganization: organizationID,
		Surface:              networkaccess.SurfacePublic,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve mcp endpoint: %w", err)
	}
	if claimed.Found {
		return nil, nil
	}

	resourceURL, err := url.JoinPath(address.baseURL, "mcp", address.slug)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build resource URL").LogError(ctx, logger)
	}
	federation := newFederationEndpoint(resourceURL)

	served, err := s.loadToolset(ctx, address.slug, address.customDomainID, false)
	switch {
	case errors.Is(err, errToolsetNotFound) || (err == nil && served.ID != toolset.ID):
		if !home {
			return nil, nil
		}
		federation.NotReady = FederationNotPubliclyReachable
		return &federation, nil
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load mcp server toolset").LogError(ctx, logger)
	}
	if !served.UserSessionIssuerID.Valid {
		federation.NotReady = FederationNoAuthorizationServer
		return &federation, nil
	}

	endpoint := newResolvedMcpEndpointFromToolset(served, "mcp")
	if err := s.RequireUserSessionIssuer(ctx, endpoint); err != nil {
		var shareable *oops.ShareableError
		if errors.As(err, &shareable) && shareable.Code == oops.CodeNotFound {
			federation.NotReady = FederationNoAuthorizationServer
			return &federation, nil
		}
		return nil, err
	}
	if err := s.discoverFederation(ctx, logger, &federation, endpoint, address.baseURL); err != nil {
		return nil, err
	}
	return &federation, nil
}

// discoverFederation fills federation from the discovery values endpoint's
// authorization server metadata serves when its resource is under baseURL.
func (s *Service) discoverFederation(
	ctx context.Context,
	logger *slog.Logger,
	federation *FederationEndpoint,
	endpoint *ResolvedMcpEndpoint,
	baseURL string,
) error {
	discovery, err := s.discoverAuthorizationServer(endpoint, baseURL)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "build OAuth server URLs").LogError(ctx, logger)
	}
	federation.Issuer = discovery.urls.Issuer
	federation.TokenEndpoint = discovery.urls.Token
	federation.OnAuthenticationHost = s.authorizationServerBaseURL(endpoint, baseURL) != baseURL
	federation.GrantTypesSupported = discovery.grantTypes
	federation.WorkloadGrantAdvertised = discovery.workloadGrant

	if !discovery.workloadGrant {
		federation.NotReady = FederationWorkloadGrantUnavailable
		return nil
	}
	rolloutEnabled, _, err := s.agentAuthorizationRollout(ctx, logger, endpoint)
	if err != nil {
		return oops.E(oops.CodeUnavailable, err, "agent authorization rollout is unavailable").LogError(ctx, logger)
	}
	if !rolloutEnabled {
		federation.NotReady = FederationAgentRolloutDisabled
	}
	return nil
}
