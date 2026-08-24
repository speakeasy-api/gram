// Package metamcp colocates the view layer of the meta-server MCP surface —
// the constants, instructions, tool contract, and response shapes served by
// meta-MCP-backed endpoints — one file per MCP method. Protocol termination
// (dispatch, version validation, envelope marshalling) stays with the mcp
// package's serve_meta handlers.
package metamcp

// MaxBodyBytes caps a meta-surface JSON-RPC request body at 1 MiB (1 << 20
// bytes), matching the generic and platform-toolset MCP surfaces: comfortably
// above any realistic tools/call argument payload while bounding per-request
// memory on an internet-reachable surface.
const MaxBodyBytes = 1 << 20

// MaxDescribeTools caps how many names one describe_tools call may request.
// The body cap alone does not bound the response: each name emits a full
// input schema, so a body of repeated short names amplifies by orders of
// magnitude.
const MaxDescribeTools = 250

// The fixed gateway tool contract. ToolDescribeTools and ToolExecuteTool
// intentionally carry the same wire names as the dynamic toolset surface's
// tools so agents see one vocabulary across Gram surfaces.
const (
	ToolListServers    = "list_servers"
	ToolDescribeServer = "describe_server"
	ToolDescribeTools  = "describe_tools"
	ToolExecuteTool    = "execute_tool"
)
