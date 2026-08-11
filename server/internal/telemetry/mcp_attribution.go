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
// authenticated org keeps one org's submission from ever being read while
// another org's staged row with the same request id is promoted.
//
// The scope is the org, not the project, because the two ends of the join
// authenticate independently: the tuple's project would come from the plugin
// hooks key's project slug (GRAM_HOOKS_PROJECT_SLUG, default "default")
// while the staged row's project comes from the OTEL exporter's key, and an
// org-wide hooks key makes the two disagree — the join would silently miss
// and every row would promote verbatim as "custom". Both paths agree on the
// org, and within one org a request id belongs to exactly one session — one
// project's OTEL — so org scoping cannot introduce intra-org collisions.
func MCPAttributionTupleKey(orgID string, requestID string) string {
	return fmt.Sprintf("mcp-attr:tuple:%s:%s", orgID, requestID)
}

// MCPPromotionClaimKey is the Redis key for one staged row's promotion claim.
// The staged-telemetry promotion path SET-NX's this key before inserting the
// row into telemetry_logs, so only one activity attempt ever inserts a given
// id. It closes the race the existence check cannot see — a timed-out attempt
// whose insert lands after a retry's check — on the non-replicated MergeTree
// deployment, where insert_deduplication_token is inert. Scoped by project
// (unlike the org-scoped tuple key) because it guards a per-project ClickHouse
// row insert: promotion passes fan out per project and row ids never cross
// projects.
func MCPPromotionClaimKey(projectID string, id string) string {
	return fmt.Sprintf("mcp-attr:promote-claim:%s:%s", projectID, id)
}
