// Package toolref parses and attributes the namespaced tool-call names that
// agent hosts emit.
//
// MCP-routed tools use either the "mcp__<server>__<function>" convention
// (Claude Code) or a "MCP:" prefix (Cursor); native tools are bare (e.g.
// "bash"). These helpers are the single source of truth for that parsing,
// shared by the hooks package (shadow_mcp matching, telemetry attribution) and
// the risk_analysis package (batch analyzer, realtime judge, and custom-rule
// engine attribution).
package toolref

import "strings"

// IsMCPToolName reports whether a tool-call name is MCP-routed. A well-formed
// name needs both a server and a function: the "mcp__<server>__<function>" form
// requires both segments non-empty, and the "MCP:<function>" form requires a
// non-empty suffix. Malformed names (e.g. "mcp____x", a bare "MCP:") are not
// MCP-routed, so they don't leak into MCP-only attribution/enforcement paths.
func IsMCPToolName(name string) bool {
	if strings.HasPrefix(name, "mcp__") {
		parts := strings.SplitN(name, "__", 3)
		return len(parts) == 3 && parts[1] != "" && parts[2] != ""
	}
	return strings.HasPrefix(name, "MCP:") && len(name) > len("MCP:")
}

// MCPFunctionOf returns the bare function name with any MCP routing prefix
// removed so it can be compared against the toolset's tool list (e.g.
// "run_task" from "mcp__mise__run_task"). Returns the name unchanged for
// non-MCP tools.
func MCPFunctionOf(name string) string {
	if strings.HasPrefix(name, "mcp__") {
		rest := name[len("mcp__"):]
		if _, after, ok := strings.Cut(rest, "__"); ok {
			return after
		}
		return rest
	}
	return strings.TrimPrefix(name, "MCP:")
}

// MCPServerOf returns the <server> portion of an MCP-routed tool name — e.g.
// "mise" from "mcp__mise__run_task" — for use as a stable, server-level
// identifier. This is what the hook-time matcher computes from tool names too,
// so a shadow_mcp finding's match column stays consistent across batch and
// hook paths. Returns "" for non-MCP tool names.
func MCPServerOf(name string) string {
	if strings.HasPrefix(name, "mcp__") {
		rest := name[len("mcp__"):]
		if before, _, ok := strings.Cut(rest, "__"); ok {
			return before
		}
		return ""
	}
	if after, ok := strings.CutPrefix(name, "MCP:"); ok {
		return after
	}
	return ""
}

// AttributeTool destructures a tool-call name for policy attribution. For
// MCP-routed names it returns the server and function components with isMCP
// true; for native tools it returns ("", "", false) and callers use the raw
// name as-is.
func AttributeTool(name string) (server, function string, isMCP bool) {
	if !IsMCPToolName(name) {
		return "", "", false
	}
	return MCPServerOf(name), MCPFunctionOf(name), true
}
