package metamcp

import "github.com/speakeasy-api/gram/server/internal/metamcp/visibility"

// Visibility values for meta_mcp_servers.visibility, aliasing the leaf
// visibility package so callers can compare against typed constants instead
// of bare string literals.
const (
	VisibilityPrivate  = visibility.Private
	VisibilityDisabled = visibility.Disabled
)
