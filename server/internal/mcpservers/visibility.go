package mcpservers

// Visibility values for mcp_servers.visibility. Mirror the enum declared
// in the design package (design/mcpservers/design.go) so callers can
// compare against typed constants instead of bare string literals.
const (
	VisibilityPublic   = "public"
	VisibilityPrivate  = "private"
	VisibilityDisabled = "disabled"
)
