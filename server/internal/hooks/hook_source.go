package hooks

import "strings"

// canonicalHookSource normalizes the gram.hook.source vocabulary at ingest
// time. Claude surfaces have historically been stamped with several spellings
// — "claude" from legacy hook rows and the unified-ingest adapter,
// "claude-code-desktop" and "cowork" from desktop/cowork service names — which
// split one agent into multiple buckets on the cost page. Collapse them all to
// the canonical "claude-code"; every other source passes through trimmed.
// Keep the Claude family list in sync with the read-side normalization in
// attribute_metrics_summaries_mv (server/clickhouse/schema.sql) and
// sessionHookSourceExpr (server/internal/telemetry/repo/sessions.go), which
// apply the same mapping to rows written before this existed.
func canonicalHookSource(source string) string {
	trimmed := strings.TrimSpace(source)
	switch trimmed {
	case "claude", "claude-code", "claude-code-desktop", "cowork":
		return "claude-code"
	default:
		return trimmed
	}
}
