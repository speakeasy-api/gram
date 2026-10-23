// Package mcproute names the URL path prefixes Speakeasy serves MCP endpoints
// under.
package mcproute

// Base is the URL path prefix an MCP endpoint is served under, followed by its
// slug. Cached OAuth state records it so a flow resumes on the surface that
// minted it.
type Base string

const (
	// MCP serves stored endpoints at /mcp/{slug}.
	MCP Base = "mcp"
	// Builtin serves code-owned project resources.
	Builtin Base = "mcp/builtin"
	// OrganizationGateway serves an organization's gateway at
	// /mcp/gateway/{organizationSlug}.
	OrganizationGateway Base = "mcp/gateway"
)

// Resolve maps a cached route base onto a served surface. Empty is MCP, for
// state cached before route bases were recorded. Any other value, such as the
// retired x/mcp, names no served surface and reports false.
func (b Base) Resolve() (Base, bool) {
	switch b {
	case "", MCP:
		return MCP, true
	case Builtin:
		return Builtin, true
	case OrganizationGateway:
		return OrganizationGateway, true
	default:
		return "", false
	}
}

func (b Base) String() string { return string(b) }
