package plugins

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

// packageMCPURL selects the same namespace the serving policy permits. A
// private-only member must never fall back to its public endpoint.
func packageMCPURL(modeValue pgtype.Text, publicBase, publicSlug, privateDNS, privateSlug string) (string, error) {
	mode, err := networkaccess.Effective(modeValue)
	if err != nil {
		return "", fmt.Errorf("resolve plugin MCP network access: %w", err)
	}
	if mode == networkaccess.ModePrivateOnly {
		if privateDNS == "" || privateSlug == "" {
			return "", fmt.Errorf("private-only plugin MCP has no endpoint in the private ingress namespace")
		}
		return "https://" + privateDNS + "/mcp/" + url.PathEscape(privateSlug), nil
	}
	if publicSlug == "" {
		return "", fmt.Errorf("plugin MCP has no public endpoint")
	}
	return publicBase + "/mcp/" + publicSlug, nil
}

// toolsetServerURL resolves a toolset-backed plugin server to the address its
// package uses. ok is false when the toolset has no MCP slug, in which case the
// server is not part of the package.
func toolsetServerURL(serverURL string, r repo.ListPluginsWithServersForProjectRow) (mcpURL string, ok bool, err error) {
	mcpSlug := conv.FromPGText[string](r.ToolsetMcpSlug)
	if mcpSlug == nil {
		return "", false, nil
	}

	mcpBase := serverURL
	if cd := conv.FromPGText[string](r.ToolsetCustomDomain); cd != nil {
		mcpBase = "https://" + *cd
	}
	switch {
	case r.WrapperCount > 1:
		return "", true, errors.New("ambiguous toolset MCP serving wrapper")
	case r.WrapperCount == 1:
		mcpURL, err = packageMCPURL(r.WrapperNetworkAccessMode, mcpBase, *mcpSlug, r.PrivateDnsName.String, r.PrivateEndpointSlug)
		if err != nil {
			return "", true, err
		}
		return mcpURL, true, nil
	default:
		return mcpBase + "/mcp/" + *mcpSlug, true, nil
	}
}

// remoteServerURL resolves an mcp_server-backed plugin server to the address
// its package uses. unproxied reports that the address is the vendor's own
// server rather than Speakeasy's gateway, so no Speakeasy credential may be
// sent to it.
//
// The unproxied URL wins whenever it is set: it is the authoritative backend
// signal, and nothing stops an mcp_endpoints row from also existing for an
// unproxied-backed server.
func remoteServerURL(serverURL string, r repo.ListPluginsWithMcpServersForProjectRow) (mcpURL string, unproxied bool, err error) {
	if r.UnproxiedUrl.Valid {
		mode, err := networkaccess.Effective(r.NetworkAccessMode)
		if err != nil {
			return "", true, fmt.Errorf("unproxied plugin MCP has invalid network mode: %w", err)
		}
		if mode != networkaccess.ModePublicOnly {
			return "", true, errors.New("unproxied plugin MCP cannot use private network mode")
		}
		return r.UnproxiedUrl.String, true, nil
	}

	// Custom-domain endpoints win on the public surface. The private surface
	// is pinned to the ingress's configured namespace instead.
	mcpBase := serverURL
	if cd := conv.FromPGText[string](r.EndpointCustomDomain); cd != nil {
		mcpBase = "https://" + *cd
	}
	mcpURL, err = packageMCPURL(r.NetworkAccessMode, mcpBase, r.EndpointSlug, r.PrivateDnsName.String, r.PrivateEndpointSlug)
	if err != nil {
		return "", false, err
	}
	return mcpURL, false, nil
}
