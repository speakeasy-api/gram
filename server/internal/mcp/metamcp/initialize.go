package metamcp

// Instructions is the server-instructions block answered by initialize and
// server/discover. Deliberately static and member-agnostic: clients cache
// instructions from the handshake, and the member set is both mutable and
// filtered per caller, so naming members here would go stale, contradict
// list_servers, and disclose members a caller cannot reach. It teaches the
// drill-down and the two rules agents get wrong; the inventory itself belongs
// to list_servers alone.
const Instructions = `This endpoint fronts a set of MCP servers as one, exposing four tools instead of every member's full catalog.

Work from the outside in:

1. list_servers — what is reachable, what each system is for, and each member's status: "available", "unavailable" when the member's tunnel is down, or "unknown" when the gateway cannot observe the member's health.
2. describe_server — that server's tools, as qualified names (server--tool) with descriptions but no input schemas.
3. describe_tools — input schemas for the specific tools you intend to call.
4. execute_tool — run one, by qualified name.

Never execute a name you have not described: arguments guessed from a tool name will fail validation.

If a call fails, re-check list_servers before retrying. "unknown" means unobserved, not broken — such members usually answer describe and execute calls normally. Only "unavailable" means the member cannot currently be reached.`
