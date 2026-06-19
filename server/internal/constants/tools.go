package constants

import "regexp"

const (
	SlugPattern = `^[a-z0-9_-]{1,128}$`
	SlugMessage = "must be lowercase, alphanumeric and can contain dashes (-) and underscores (_)"
	// MCPToolNamePattern follows the MCP 2025-11-25 tool name format. Gram URN
	// kind/source segments remain slug-like; only the tool name segment uses this.
	MCPToolNamePattern     = `^[A-Za-z0-9_.-]{1,128}$`
	MCPToolNameMessage     = "must be 1-128 characters and can contain letters, digits, underscores (_), dashes (-), and periods (.)"
	DefaultEmptyToolSchema = `{"type":"object","properties":{}}`
)

var (
	SlugPatternRE        = regexp.MustCompile(SlugPattern)
	MCPToolNamePatternRE = regexp.MustCompile(MCPToolNamePattern)
)
