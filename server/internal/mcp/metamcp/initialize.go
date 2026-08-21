package metamcp

// Instructions is the server-instructions block answered by initialize and
// server/discover. Deliberately static and generic: clients may cache
// instructions upfront, so they must never be treated as authoritative over
// tool results — the member inventory belongs to list_servers alone.
const Instructions = "This server has dynamic results and requires dynamic tool calls to discover and execute underlying functionality. Discovery is a drill-down: call list_servers to see the member server inventory, describe_server for one member's tool catalog, describe_tools for the full input schemas of named tools, then execute_tool to run one. Perform rediscovery on unexpected errors."
