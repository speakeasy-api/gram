package shadowmcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
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

// TrustedMCPHostsForOrg returns the hosts that count as Gram-hosted for an
// organization on top of the built-in ones: the deployment's own host and the
// org's verified, activated custom domain.
//
// Callers that classify many URLs for one organization should resolve this
// once and pass it to [IsGramHostedMCPURL], rather than calling
// [Client.IsGramHostedMCPURLForOrg] per URL — the custom-domain lookup is a
// database round-trip whose result is invariant for the organization.
//
// The error is returned rather than swallowed. A transient custom-domain
// lookup failure is not the same answer as "this org has no custom domain":
// treating it as the latter classifies calls to the org's own verified domain
// as shadow MCP. Callers that would act on a negative must treat an error as
// "host resolution unavailable" and fall back to whatever they do when
// provenance is unknown.
func (c *Client) TrustedMCPHostsForOrg(ctx context.Context, orgID string) ([]string, error) {
	hosts := make([]string, 0, 2)
	if c.serverURL != nil && c.serverURL.Host != "" {
		hosts = append(hosts, c.serverURL.Host)
	}
	if orgID == "" {
		return hosts, nil
	}

	customDomain, err := customdomainsrepo.New(c.db).GetCustomDomainByOrganization(ctx, orgID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return hosts, nil
	case err != nil:
		return nil, fmt.Errorf("get custom domain for organization: %w", err)
	}
	if !customDomain.Verified || !customDomain.Activated {
		return hosts, nil
	}
	return append(hosts, customDomain.Domain), nil
}

// IsGramHostedMCPURLForOrg reports whether rawURL is a Gram-managed MCP server
// for the given organization. It checks the canonical and configured hosts
// first (no DB hit), then falls back to the org's verified custom domain.
//
// This is the definition of "Gram-hosted" shared by the realtime hook guard
// and the offline batch scanner, so a call the hook allows is not flagged by
// the scanner on host grounds alone. Use it for one-off classifications; for
// many URLs under one organization, see [Client.TrustedMCPHostsForOrg].
//
// A custom-domain lookup failure is logged and reported as not hosted, which
// makes the realtime guard fail closed (deny) on an infrastructure blip. That
// is the right direction for a single in-flight request but not for the batch
// scanner, whose verdicts persist as findings — it calls
// [Client.TrustedMCPHostsForOrg] directly and handles the error itself.
func (c *Client) IsGramHostedMCPURLForOrg(ctx context.Context, rawURL, orgID string) bool {
	if rawURL == "" {
		return false
	}
	// Fast path: check built-in and configured hosts before the DB round-trip.
	if IsGramHostedMCPURL(rawURL, c.serverHost()...) {
		return true
	}
	if orgID == "" {
		return false
	}

	hosts, err := c.TrustedMCPHostsForOrg(ctx, orgID)
	if err != nil {
		c.logger.ErrorContext(ctx, "resolve organization trusted MCP hosts; treating URL as not Gram-hosted",
			attr.SlogError(err),
			attr.SlogOrganizationID(orgID),
		)
		return false
	}
	return IsGramHostedMCPURL(rawURL, hosts...)
}

func (c *Client) serverHost() []string {
	if c.serverURL == nil || c.serverURL.Host == "" {
		return nil
	}
	return []string{c.serverURL.Host}
}
