package shadowmcp

import (
	"context"
	"net/url"
	"strings"

	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
)

// gramHostedMCPHosts are the built-in hosts for Gram-managed MCP servers.
// Exact host matching keeps third-party subdomains from being treated as
// trusted Gram-hosted MCP servers.
var gramHostedMCPHosts = []string{
	"app.getgram.ai",
	"chat.speakeasy.com",
}

// IsGramHostedMCPURL reports whether rawURL points at a Gram-managed MCP
// server. Checks the canonical hosts plus any additional trusted hosts.
// Exact host match, case-insensitive.
//
// Matching is on the host, never on the path: a path-shaped check would
// classify https://evil.example.com/mcp/<known-slug> as Gram-hosted.
func IsGramHostedMCPURL(rawURL string, additionalTrustedHosts ...string) bool {
	if rawURL == "" {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	hostname := u.Hostname()
	for _, h := range gramHostedMCPHosts {
		if strings.EqualFold(hostname, h) {
			return true
		}
	}
	for _, h := range additionalTrustedHosts {
		if trustedGramHostedMCPHostMatches(u, h) {
			return true
		}
	}
	return false
}

func trustedGramHostedMCPHostMatches(u *url.URL, trustedHost string) bool {
	trustedHost = strings.TrimSpace(trustedHost)
	if trustedHost == "" {
		return false
	}
	if strings.Contains(trustedHost, "://") {
		parsed, err := url.Parse(trustedHost)
		if err != nil || parsed.Host == "" {
			return false
		}
		trustedHost = parsed.Host
	}
	if strings.Contains(trustedHost, ":") {
		return strings.EqualFold(u.Host, trustedHost)
	}
	return strings.EqualFold(u.Hostname(), trustedHost)
}

// IsGramHostedMCPURLForOrg reports whether rawURL is a Gram-managed MCP server
// for the given organization. It checks the canonical and configured hosts
// first (no DB hit), then falls back to the org's verified custom domain.
//
// This is the single definition of "Gram-hosted" shared by the realtime hook
// guard and the offline batch scanner, so a call the hook allows can never be
// flagged by the scanner on host grounds alone.
func (c *Client) IsGramHostedMCPURLForOrg(ctx context.Context, rawURL, orgID string) bool {
	trustedHosts := make([]string, 0, 1)
	if c.serverURL != nil && c.serverURL.Host != "" {
		trustedHosts = append(trustedHosts, c.serverURL.Host)
	}

	// Fast path: check built-in/configured hosts before consulting custom domains.
	if IsGramHostedMCPURL(rawURL, trustedHosts...) {
		return true
	}
	if rawURL == "" || orgID == "" {
		return false
	}
	customDomain, err := customdomainsrepo.New(c.db).GetCustomDomainByOrganization(ctx, orgID)
	if err != nil || !customDomain.Verified || !customDomain.Activated {
		return false
	}
	return IsGramHostedMCPURL(rawURL, customDomain.Domain)
}
