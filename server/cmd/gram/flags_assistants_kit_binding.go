package gram

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/assistants"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// validateKitMCPBinding runs before credential rotation. Resolve the served
// address and its issuer from durable state, not from matching slugs alone.
func validateKitMCPBinding(ctx context.Context, db toolsetsrepo.DBTX, request assistants.KitMCPProvisionRequest, resourceURL, issuerURL *url.URL, hosts authserver.Hosts, localOrigin *url.URL) error {
	if resourceURL == nil || issuerURL == nil || resourceURL.Scheme != "https" || resourceURL.Host == "" || resourceURL.User != nil || resourceURL.RawQuery != "" || resourceURL.Fragment != "" || resourceURL.RawPath != "" {
		return errors.New("kit MCP requires an exact HTTPS resource address")
	}
	canonicalResource := *resourceURL
	canonicalResource.Host = strings.ToLower(canonicalResource.Host)
	resourceURL = &canonicalResource
	slug := strings.TrimPrefix(resourceURL.Path, "/mcp/")
	if !strings.HasPrefix(resourceURL.Path, "/mcp/") || slug == "" || strings.Contains(slug, "/") {
		return errors.New("kit MCP requires an attached /mcp/<slug> resource")
	}
	origin := resourceURL.Scheme + "://" + resourceURL.Host
	var domainID uuid.NullUUID
	if !kitMCPPlatformOrigin(origin, hosts, localOrigin) {
		// Custom-domain addresses must be owned and actively served by this tenant.
		if resourceURL.Port() != "" {
			return errors.New("kit MCP resource origin is not served")
		}
		domain, err := customdomainsrepo.New(db).GetCustomDomainByDomainAndOrganization(ctx, customdomainsrepo.GetCustomDomainByDomainAndOrganizationParams{Domain: resourceURL.Hostname(), OrganizationID: request.OrganizationID})
		if err != nil {
			return fmt.Errorf("resolve Kit MCP resource origin: %w", err)
		}
		if domain.Domain != resourceURL.Hostname() || domain.OrganizationID != request.OrganizationID || !domain.Verified || !domain.Activated {
			return errors.New("kit MCP resource domain is not active for this organization")
		}
		domainID = uuid.NullUUID{UUID: domain.ID, Valid: true}
	}
	endpoint, err := mcpendpointsrepo.New(db).GetMCPEndpointByProjectAndCustomDomainAndSlug(ctx, mcpendpointsrepo.GetMCPEndpointByProjectAndCustomDomainAndSlugParams{ProjectID: request.ProjectID, CustomDomainID: domainID, Slug: slug})
	if err == nil && endpoint.ProjectID != request.ProjectID {
		return errors.New("kit MCP endpoint belongs to another project")
	}
	var boundIssuer uuid.NullUUID
	switch {
	case err == nil && endpoint.McpServerID.Valid:
		server, err := mcpserversrepo.New(db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: endpoint.McpServerID.UUID, ProjectID: request.ProjectID})
		if err != nil {
			return fmt.Errorf("resolve Kit MCP server: %w", err)
		}
		if server.Visibility == "disabled" || (server.TunneledMcpServerID.Valid && server.Visibility == "public") || server.UnproxiedMcpServerID.Valid {
			return errors.New("kit MCP resource does not serve workload OAuth")
		}
		mode, err := networkaccess.Effective(server.NetworkAccessMode)
		if err != nil || !mode.Allows(networkaccess.SurfacePublic) {
			return errors.New("kit MCP resource does not allow public network access")
		}
		boundIssuer = server.UserSessionIssuerID
	case err == nil && endpoint.MetaMcpServerID.Valid:
		server, err := metamcprepo.New(db).GetMetaMCPServerByIDAndProjectID(ctx, metamcprepo.GetMetaMCPServerByIDAndProjectIDParams{ID: endpoint.MetaMcpServerID.UUID, ProjectID: request.ProjectID})
		if err != nil {
			return fmt.Errorf("resolve Kit meta MCP server: %w", err)
		}
		if server.Visibility == "disabled" {
			return errors.New("kit MCP resource is disabled")
		}
		mode, err := networkaccess.Effective(server.NetworkAccessMode)
		if err != nil || !mode.Allows(networkaccess.SurfacePublic) {
			return errors.New("kit MCP resource does not allow public network access")
		}
		boundIssuer = server.UserSessionIssuerID
	case errors.Is(err, pgx.ErrNoRows):
		// Match the public route's legacy toolset fallback without crossing domains.
		toolset, err := toolsetsrepo.New(db).GetToolsetByMcpSlugAndProjectAndCustomDomain(ctx, toolsetsrepo.GetToolsetByMcpSlugAndProjectAndCustomDomainParams{OrganizationID: request.OrganizationID, CustomDomainID: domainID, McpSlug: pgtype.Text{String: slug, Valid: true}, ProjectID: request.ProjectID})
		if err != nil {
			return fmt.Errorf("resolve Kit MCP toolset: %w", err)
		}
		toolset, _, err = hostedmcp.LoadEffective(ctx, db, toolset)
		if err != nil {
			return fmt.Errorf("resolve Kit MCP toolset server: %w", err)
		}
		if toolset.ProjectID != request.ProjectID || toolset.CustomDomainID != domainID || !toolset.McpEnabled {
			return errors.New("kit MCP resource is not enabled in this project")
		}
		boundIssuer = toolset.UserSessionIssuerID
	default:
		if err != nil {
			return fmt.Errorf("resolve Kit MCP endpoint: %w", err)
		}
		return errors.New("kit MCP endpoint has no supported server target")
	}
	if !boundIssuer.Valid || boundIssuer.UUID != request.UserSessionIssuerID {
		return errors.New("kit MCP resource does not use the configured user-session issuer")
	}
	row, err := usersessionsrepo.New(db).GetKitUserSessionIssuerByID(ctx, usersessionsrepo.GetKitUserSessionIssuerByIDParams{ID: boundIssuer.UUID, ProjectID: request.ProjectID, OrganizationID: request.OrganizationID})
	if err != nil {
		return fmt.Errorf("resolve Kit MCP issuer: %w", err)
	}

	return validateKitMCPIssuerURL(resourceURL, issuerURL, row, hosts, domainID.Valid)
}

func kitMCPPlatformOrigin(origin string, hosts authserver.Hosts, localOrigin *url.URL) bool {
	if strings.EqualFold(origin, strings.TrimSuffix(hosts.ServerURL, "/")) {
		return true
	}
	for _, value := range hosts.PlatformHosts {
		if strings.EqualFold(origin, strings.TrimSuffix(value, "/")) {
			return true
		}
	}
	// Local runners use the explicitly configured host-gateway/relay address.
	// It is not an extra production authority or a shared authorization host.
	return localOrigin != nil && strings.EqualFold(origin, localOrigin.Scheme+"://"+localOrigin.Host)
}

func validateKitMCPIssuerURL(resourceURL, expected *url.URL, issuer usersessionsrepo.UserSessionIssuer, hosts authserver.Hosts, customDomain bool) error {
	actual := resourceURL.String()
	if authserver.IssuerInSharedMode(issuer) {
		var err error
		actual, err = hosts.SharedIssuerURL(issuer)
		if err != nil {
			return fmt.Errorf("resolve Kit shared issuer: %w", err)
		}
	} else if issuer.AuthorizationServerMode != string(authserver.ModeEndpoint) {
		return errors.New("kit MCP issuer mode is unsupported")
	} else if issuer.UseAuthenticationHost && !customDomain && hosts.AuthenticationHostBaseURL != "" && strings.EqualFold(resourceURL.Scheme+"://"+resourceURL.Host, strings.TrimSuffix(hosts.ServerURL, "/")) {
		actual = strings.TrimSuffix(hosts.AuthenticationHostBaseURL, "/") + resourceURL.Path
	}
	actualURL, err := url.Parse(actual)
	if expected == nil || err != nil || !strings.EqualFold(expected.Host, actualURL.Host) || expected.Scheme != actualURL.Scheme || expected.RequestURI() != actualURL.RequestURI() || expected.User != nil || expected.Fragment != "" {
		return errors.New("kit MCP expected issuer URL does not match the resource's authoritative issuer")
	}
	return nil
}
