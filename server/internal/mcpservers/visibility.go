package mcpservers

import "github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"

// Visibility values for mcp_servers.visibility, aliasing the leaf visibility
// package so callers can compare against typed constants instead of bare
// string literals. Packages that mcpservers itself depends on (e.g. plugins)
// import the visibility package directly to avoid an import cycle.
const (
	VisibilityPublic   = visibility.Public
	VisibilityPrivate  = visibility.Private
	VisibilityDisabled = visibility.Disabled
)
