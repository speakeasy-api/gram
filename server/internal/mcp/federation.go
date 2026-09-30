// What an external platform must be pointed at to exchange its workload
// identity token for a session on one of an MCP server's addresses.

package mcp

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	mcpendpoints_repo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/oops"
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
// outside the organization could federate with, platform-host addresses
// first. Each is resolved as a public request for it would be, and its values
// come from the same derivation as the endpoint's discovery document. The
// caller is responsible for authorizing the read of server.
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
		baseURL := strings.TrimSuffix(s.serverURL.String(), "/")
		namespace := mcpendpoints.NamespacePlatform
		if row.McpEndpoint.CustomDomainID.Valid {
			baseURL = "https://" + row.CustomDomain.String
			namespace = mcpendpoints.NamespaceCustomDomain
		}
		endpoint, err := s.federationEndpoint(ctx, logger, organizationID, server.ID, row.McpEndpoint, baseURL, namespace)
		if err != nil {
			return nil, err
		}
		endpoints = append(endpoints, endpoint)
	}
	return endpoints, nil
}

func (s *Service) federationEndpoint(
	ctx context.Context,
	logger *slog.Logger,
	organizationID string,
	serverID uuid.UUID,
	address mcpendpoints_repo.McpEndpoint,
	baseURL string,
	namespace mcpendpoints.NamespaceKind,
) (FederationEndpoint, error) {
	resourceURL, err := url.JoinPath(baseURL, "mcp", address.Slug)
	if err != nil {
		return FederationEndpoint{}, oops.E(oops.CodeUnexpected, err, "build resource URL").LogError(ctx, logger)
	}
	federation := FederationEndpoint{
		ResourceURL:             resourceURL,
		Issuer:                  "",
		TokenEndpoint:           "",
		OnAuthenticationHost:    false,
		GrantTypesSupported:     []string{},
		WorkloadGrantAdvertised: false,
		NotReady:                FederationReady,
	}

	resolved, err := mcpendpoints.Resolve(ctx, s.db, logger, mcpendpoints.ResolutionInput{
		Slug:                 address.Slug,
		NamespaceKind:        namespace,
		CustomDomainID:       address.CustomDomainID,
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
	discovery, err := s.discoverAuthorizationServer(endpoint, baseURL)
	if err != nil {
		return FederationEndpoint{}, oops.E(oops.CodeUnexpected, err, "build OAuth server URLs").LogError(ctx, logger)
	}
	federation.Issuer = discovery.urls.Issuer
	federation.TokenEndpoint = discovery.urls.Token
	federation.OnAuthenticationHost = s.authorizationServerBaseURL(endpoint, baseURL) != baseURL
	federation.GrantTypesSupported = discovery.grantTypes
	federation.WorkloadGrantAdvertised = discovery.workloadGrant

	if !discovery.workloadGrant {
		federation.NotReady = FederationWorkloadGrantUnavailable
		return federation, nil
	}
	rolloutEnabled, _, err := s.agentAuthorizationRollout(ctx, logger, endpoint)
	if err != nil {
		return FederationEndpoint{}, oops.E(oops.CodeUnavailable, err, "agent authorization rollout is unavailable").LogError(ctx, logger)
	}
	if !rolloutEnabled {
		federation.NotReady = FederationAgentRolloutDisabled
	}
	return federation, nil
}
