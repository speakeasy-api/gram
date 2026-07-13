package telemetry

import (
	"fmt"
	"time"
)

// MCPAttributionTuple is one transcript-derived MCP attribution fact: the
// unredacted server/tool names Claude recorded in the local session transcript
// for a model API request whose OTEL telemetry was stamped
// mcp_server.name='custom'. Written to Redis by the hooks ingest path
// (Stop/SubagentStop/SessionEnd) and read by the staged-telemetry promotion worker, which
// is why the type and key live here rather than in the hooks package: hooks
// already depends on background, so background/activities cannot import hooks.
type MCPAttributionTuple struct {
	Server string `json:"server"`
	Tool   string `json:"tool"`
}

// MCPAttributionTupleTTL bounds how long a tuple waits for its OTEL row. The
// row's final export batch lands within seconds of the Stop hook; hours of
// headroom cover collector re-batching and retries without accumulating keys.
const MCPAttributionTupleTTL = 2 * time.Hour

// MCPAttributionTupleKey is the Redis key for one request's attribution
// tuple. Request ids (Claude req_*) are globally unique, but the request id
// arrives on an untrusted client payload — scoping the key by the
// authenticated project keeps one project's submission from ever being read
// while another project's staged row with the same request id is promoted.
func MCPAttributionTupleKey(projectID string, requestID string) string {
	return fmt.Sprintf("mcp-attr:tuple:%s:%s", projectID, requestID)
}
