package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/customdomains"
	domainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
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

// WorkloadTokenTargetFor resolves the workload token target of the MCP server
// at mcpURL, a `<base>/mcp/<slug>` address on the platform host or a custom
// domain. ok is false when no workload grant reaches the server from there: it
// is not issuer-gated, its authorization server offers no workload grant, or
// it is addressed on a private network ingress, whose authorization server
// this public lookup cannot stand in for.
//
// A shared authorization server is preferred whenever the issuer is in shared
// mode, even where browser clients are kept on per-endpoint servers: it mints
// one session for every server of the issuer, and on the authentication host
// it serves nothing but this grant.
func (s *Service) WorkloadTokenTargetFor(ctx context.Context, mcpURL string) (WorkloadTokenTarget, bool, error) {
	var none WorkloadTokenTarget
	u, err := url.Parse(mcpURL)
	if err != nil {
		return none, false, fmt.Errorf("parse MCP server URL: %w", err)
	}
	slug, isSlugPath := strings.CutPrefix(u.EscapedPath(), "/mcp/")
	if !isSlugPath || slug == "" || strings.Contains(slug, "/") {
		return none, false, nil
	}
	if slug, err = url.PathUnescape(slug); err != nil {
		return none, false, fmt.Errorf("unescape MCP server slug: %w", err)
	}

	ctx, baseURL, ok, err := s.publicOriginContext(ctx, u.Host)
	if err != nil || !ok {
		return none, false, err
	}

	endpoint, err := s.LoadResolvedMcpEndpointBySlug(ctx, s.logger, slug, "mcp")
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

	if shared := endpoint.sharedAuthorizationServer; shared != nil {
		urls, err := shared.urls()
		if err != nil {
			return none, false, fmt.Errorf("build shared authorization server URLs: %w", err)
		}
		return WorkloadTokenTarget{TokenEndpoint: urls.Token, Audience: urls.Token, Resource: ""}, true, nil
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

// publicOriginContext stamps ctx with the request origin a request to host
// would carry, as the custom-domain middleware does, and returns that origin's
// base URL. ok is false for a host that is neither a platform host nor an
// active custom domain, which includes every private network ingress.
func (s *Service) publicOriginContext(ctx context.Context, rawHost string) (context.Context, string, bool, error) {
	host, err := requestorigin.CanonicalHost(rawHost)
	if err != nil {
		return ctx, "", false, fmt.Errorf("canonicalize MCP server host: %w", err)
	}
	platformHost, _ := requestorigin.CanonicalHost(s.serverURL.Host)
	baseURL, isPlatform := s.platformHosts[host]
	if host == platformHost {
		baseURL, isPlatform = strings.TrimSuffix(s.serverURL.String(), "/"), true
	}
	if isPlatform {
		return requestorigin.WithContext(ctx, requestorigin.Origin{
			Surface:          requestorigin.SurfacePlatform,
			BaseURL:          baseURL,
			OrganizationID:   "",
			NetworkIngressID: uuid.Nil,
			NetworkIdentity:  nil,
		}), baseURL, true, nil
	}

	domain, err := domainsrepo.New(s.db).GetCustomDomainByDomain(ctx, host)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ctx, "", false, nil
	case err != nil:
		return ctx, "", false, fmt.Errorf("load custom domain: %w", err)
	case !domain.Activated || !domain.Verified:
		return ctx, "", false, nil
	}
	baseURL = "https://" + host
	ctx = customdomains.WithContext(ctx, &customdomains.Context{
		OrganizationID: domain.OrganizationID,
		Domain:         domain.Domain,
		DomainID:       domain.ID,
	})
	return requestorigin.WithContext(ctx, requestorigin.Origin{
		Surface:          requestorigin.SurfaceCustomDomain,
		BaseURL:          baseURL,
		OrganizationID:   domain.OrganizationID,
		NetworkIngressID: uuid.Nil,
		NetworkIdentity:  nil,
	}), baseURL, true, nil
}
