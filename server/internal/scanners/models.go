package scanners

// Finding represents a single secret or sensitive data match found in a message.
type Finding struct {
	RuleID           string
	Description      string
	Match            string
	StartPos         int
	EndPos           int
	Tags             []string
	Source           string
	Confidence       float64
	DeadLetterReason string

	McpLookupToolCallID string
	SpanGroupKey        string
	Field               string
	Path                string
}

// Surface values published on Finding.surface and stored in the ClickHouse
// risk_findings.surface column — which text the finding's start_pos/end_pos
// offsets index. Kept in sync with the column's schema comment
// (server/clickhouse/schema.sql) and the backfill's transform
// (server/cmd/tools/migrations/riskfindings).
const (
	SurfaceContent  = "content"
	SurfaceToolArgs = "tool_args"
	SurfaceJSONPath = "json_path"
	SurfaceDerived  = "derived"
	SurfaceNone     = "none"
)

// FindingSurface maps a finding's span attribution (field, path) — or, when
// the scanner records no spans, its source — to the text its offsets index.
// The span-level arms mirror the offline backfill's spanSurface so live rows
// and backfilled rows agree; the per-source defaults differ deliberately
// because live stream scanners index different text than the legacy sync-era
// rows the backfill covers (e.g. the gitleaks stream handler scans the verbatim
// request content, not the batch-composed scan surface). An unrecognized field
// (e.g. tool.name) leaves the surface empty so reveal falls back to its
// verified candidate cascade rather than slicing the wrong text.
//
// Source names are literals (same precedent as the categories package) because
// the scanner subpackages import this one. Canonical definitions:
//
//	gitleaks          server/internal/scanners/gitleaks.Source
//	presidio          pystreams/src/pystreams/risk/handler.py SOURCE_PRESIDIO
//	prompt_injection  server/internal/scanners/promptinjection.Source
//	llm_judge         server/internal/scanners/promptpolicy.Source
//	shadow_mcp        server/internal/scanners/shadowmcpscan.Source
//	account_identity  server/internal/background/activities/risk_analysis.SourceAccountIdentity
//	destructive_tool  server/internal/scanners/destructivetool.Source
//	cli_destructive   server/internal/scanners/clidestructive.Source
func FindingSurface(source, field, path string) string {
	switch {
	case path != "":
		return SurfaceJSONPath
	case field == "tool.args":
		return SurfaceToolArgs
	case field == "content", field == "prompt", field == "assistant", field == "tool_result":
		return SurfaceContent
	case field == "tool.server", field == "tool.function":
		return SurfaceDerived
	case field != "":
		return ""
	}

	switch source {
	case "gitleaks", "presidio", "prompt_injection":
		// These scanners match against the request's verbatim content, so the
		// offsets index the anchored message/part text (prompt_injection flags
		// the whole scanned content).
		return SurfaceContent
	case "llm_judge":
		// The judge's match is a rendered artifact of the judged content; the
		// rationale carries the signal and the offsets index no stored text.
		return SurfaceNone
	case "shadow_mcp", "account_identity", "destructive_tool", "cli_destructive":
		// The match is derived metadata (server identifier, account email,
		// tool name), not a slice of any recorded surface.
		return SurfaceDerived
	default:
		return ""
	}
}
