package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

// WorkloadTokenTarget is where a workload exchanges an identity assertion for
// a session that reaches an MCP server.
type WorkloadTokenTarget struct {
	// TokenEndpoint receives the JWT bearer grant.
	TokenEndpoint string
	// Audience is the `aud` the assertion must carry.
	Audience string
	// Resource is the resource indicator the grant must name. Empty for an
	// issuer-wide grant at a shared authorization server.
	Resource string
}

// WorkloadTokenTargetFor resolves the token target for the MCP server at mcpURL.
// ok is false when no workload grant reaches it. A shared authorization server
// is preferred because one session then covers every server of the issuer.
func (s *Service) WorkloadTokenTargetFor(ctx context.Context, mcpURL string) (WorkloadTokenTarget, bool, error) {
	var none WorkloadTokenTarget
	u, err := url.Parse(mcpURL)
	if err != nil {
		return none, false, fmt.Errorf("parse MCP server URL: %w", err)
	}
	organizationGatewaySlug, isOrganizationGateway := strings.CutPrefix(u.EscapedPath(), "/mcp/gateway/")
	slug, isSlugPath := strings.CutPrefix(u.EscapedPath(), "/mcp/")
	switch {
	case isOrganizationGateway:
		escapedOrganization, escapedProject, scoped := strings.Cut(organizationGatewaySlug, "/projects/")
		if !scoped || escapedOrganization == "" || escapedProject == "" || strings.Contains(escapedOrganization, "/") || strings.Contains(escapedProject, "/") {
			return none, false, nil
		}
		organization, unescapeErr := url.PathUnescape(escapedOrganization)
		if unescapeErr != nil {
			return none, false, fmt.Errorf("unescape gateway organization slug: %w", unescapeErr)
		}
		project, unescapeErr := url.PathUnescape(escapedProject)
		if unescapeErr != nil {
			return none, false, fmt.Errorf("unescape gateway project slug: %w", unescapeErr)
		}
		if strings.Contains(organization, "/") || strings.Contains(project, "/") {
			return none, false, nil
		}
		organizationGatewaySlug = organization + "/projects/" + project
	case !isSlugPath || slug == "" || strings.Contains(slug, "/"):
		return none, false, nil
	default:
		if slug, err = url.PathUnescape(slug); err != nil {
			return none, false, fmt.Errorf("unescape MCP server slug: %w", err)
		}
	}

	ctx, baseURL, ok, err := s.publicOriginContext(ctx, u.Host)
	if err != nil || !ok {
		return none, false, err
	}

	var endpoint *ResolvedMcpEndpoint
	if isOrganizationGateway {
		gateway, loadErr := s.loadOrganizationGateway(ctx, organizationGatewaySlug)
		if loadErr == nil {
			endpoint = gateway.resolved
		}
		err = loadErr
	} else {
		endpoint, err = s.LoadResolvedMcpEndpointBySlug(ctx, s.logger, slug, "mcp")
	}
	var oopsErr *oops.ShareableError
	switch {
	case errors.As(err, &oopsErr) && oopsErr.Code == oops.CodeNotFound:
		return none, false, nil
	case err != nil:
		return none, false, fmt.Errorf("resolve MCP endpoint: %w", err)
	}
	if !s.workloadAssertionGrantAdvertised(endpoint) {
		return none, false, nil
	}
	gateway, gatewayErr := gatewayTargetFromEndpoint(endpoint)
	if gatewayErr == nil {
		enabled, rolloutErr := s.gatewayMachineAccess(ctx, gateway)
		if rolloutErr != nil {
			return none, false, fmt.Errorf("check gateway machine access rollout: %w", rolloutErr)
		}
		if !enabled {
			return none, false, nil
		}
	}

	if shared := endpoint.sharedAuthorizationServer; shared != nil {
		urls, err := shared.urls()
		if err != nil {
			return none, false, fmt.Errorf("build shared authorization server URLs: %w", err)
		}
		resource := ""
		if gatewayErr == nil {
			if _, ok := gateway.authorizationTarget(endpoint.ProjectID); ok {
				resource, err = endpoint.RootURL(baseURL)
				if err != nil {
					return none, false, fmt.Errorf("build gateway resource URL: %w", err)
				}
			}
		}
		return WorkloadTokenTarget{TokenEndpoint: urls.Token, Audience: urls.Token, Resource: resource}, true, nil
	}

	urls, err := s.requestAuthorizationServerURLs(ctx, endpoint, baseURL)
	if err != nil {
		return none, false, fmt.Errorf("build authorization server URLs: %w", err)
	}
	resource, err := endpoint.RootURL(baseURL)
	if err != nil {
		return none, false, fmt.Errorf("build resource URL: %w", err)
	}
	return WorkloadTokenTarget{TokenEndpoint: urls.Token, Audience: urls.Token, Resource: resource}, true, nil
}

// publicOriginContext stamps ctx with the origin a request to host would carry.
// ok is false for anything but a platform host or an active custom domain.
func (s *Service) publicOriginContext(ctx context.Context, rawHost string) (context.Context, string, bool, error) {
	host, err := requestorigin.CanonicalHost(rawHost)
	if err != nil {
		return ctx, "", false, fmt.Errorf("canonicalize MCP server host: %w", err)
	}
	// Classified exactly as a shared-resource request would be, which also
	// clears any custom domain or authentication host the poll arrived on.
	resourceCtx, origin, rejection, err := s.sharedResourceContext(ctx, host)
	switch {
	case err != nil:
		return ctx, "", false, err
	case rejection != sharedResourceAccepted:
		return ctx, "", false, nil
	}
	return resourceCtx, origin.BaseURL, true, nil
}
