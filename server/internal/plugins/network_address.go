package plugins

import (
	"fmt"
	"net/url"

	"github.com/jackc/pgx/v5/pgtype"

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
