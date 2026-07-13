// Package visibility declares the mcp_servers.visibility enum values.
// It mirrors the enum in the design package (design/mcpservers/design.go)
// and is a leaf package so that packages mcpservers itself depends on
// (e.g. plugins) can reference the values without an import cycle. Most
// callers should keep using the mcpservers.Visibility* aliases.
package visibility

const (
	Public   = "public"
	Private  = "private"
	Disabled = "disabled"
)
