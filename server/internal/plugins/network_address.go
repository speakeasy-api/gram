package plugins

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
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

// remoteServerURL resolves a plugin server's package address. unproxied means
// the address is the vendor's own server, which must never get a Speakeasy
// credential; it wins whenever set, even if an mcp_endpoints row also exists.
func remoteServerURL(serverURL string, r pluginMCPServerRow) (mcpURL string, unproxied bool, err error) {
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
	// is pinned to the ingress's configured namespace instead. A server
	// address, when set, overrides the endpoint's slug and domain.
	mcpBase, publicSlug, domain := serverURL, r.EndpointSlug, r.EndpointCustomDomain
	if r.AddressSlug.Valid {
		publicSlug, domain = r.AddressSlug.String, r.AddressCustomDomain
	}
	if cd := conv.FromPGText[string](domain); cd != nil {
		mcpBase = "https://" + *cd
	}
	mcpURL, err = packageMCPURL(r.NetworkAccessMode, mcpBase, publicSlug, r.PrivateDnsName.String, r.PrivateEndpointSlug)
	if err != nil {
		return "", false, err
	}
	return mcpURL, false, nil
}
