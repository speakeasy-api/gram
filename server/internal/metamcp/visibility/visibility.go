// Package visibility declares the meta_mcp_servers.visibility enum values.
// It mirrors the enum in the design package (design/metamcp/design.go) and is
// a leaf package so the endpoint resolution path can reference the values
// without importing the metamcp service. Most callers should keep using the
// metamcp.Visibility* aliases.
//
// The vocabulary is deliberately narrower than mcp_servers.visibility: a
// gateway always requires an authenticated caller.
package visibility

const (
	Private  = "private"
	Disabled = "disabled"
)
