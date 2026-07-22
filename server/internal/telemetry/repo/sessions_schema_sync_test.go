package repo

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The session classification logic lives in several places that must never
// drift: the session* constants in this package (the raw ListSessions path),
// the chat_session_summaries_mv definition in schema.sql (ingest for the
// summary ListSessions path), and the backfill/seed SQL derived from the MV.
// This test pins the shared SQL fragments on both sides — editing the Go
// constants or the MV without moving the other breaks it, forcing the change
// to be applied everywhere (including a MODIFY QUERY migration + backfill for
// the MV; see the clickhouse skill).
var sessionSharedPredicateFragments = []string{
	// Claude OTEL provenance URN.
	"(gram_urn = 'claude-code:otel:logs')",
	// api_request row markers.
	"(toString(attributes.event.name) = 'api_request' OR body = 'claude_code.api_request')",
	// tool_result row markers.
	"(toString(attributes.event.name) = 'tool_result' OR body = 'claude_code.tool_result')",
	// Agent usage-row URN prefixes.
	"(startsWith(gram_urn, 'codex:usage') OR startsWith(gram_urn, 'cursor:usage') OR startsWith(gram_urn, 'claude_chat:usage') OR startsWith(gram_urn, 'claude_chat:cost'))",
	// Agent completed tool-call hook rows.
	"hook_source IN ('codex', 'cursor') AND toString(attributes.gram.tool.name) != '' AND toString(attributes.gram.tool.name) NOT IN ('claude-code', 'codex', 'cursor') AND toString(attributes.gram.hook.event) IN ('PostToolUse', 'PostToolUseFailure')",
	// Tool-call dedup identity.
	"multiIf(toString(attributes.tool_use_id) != '', toString(attributes.tool_use_id), toString(attributes.gen_ai.tool.call.id) != '', toString(attributes.gen_ai.tool.call.id), toString(id))",
	// Failed tool-call markers.
	"toString(attributes.success) = 'false'",
	"(toString(attributes.gram.hook.event) = 'PostToolUseFailure' OR toInt32OrZero(toString(attributes.http.response.status_code)) >= 400)",
	// Cost fallback chain.
	"multiIf(toString(attributes.cost_usd) != '', toFloat64OrZero(toString(attributes.cost_usd)), toString(attributes.cost_usd_micros) != '', toFloat64OrZero(toString(attributes.cost_usd_micros)) / 1000000, 0)",
	// total_tokens = input + output + cache writes.
	"toInt64OrZero(toString(attributes.input_tokens)) + toInt64OrZero(toString(attributes.output_tokens)) + toInt64OrZero(toString(attributes.cache_creation_tokens))",
}

// normalizeSQL collapses whitespace so the multi-line schema formatting
// compares equal to the single-line Go constants.
func normalizeSQL(s string) string {
	s = regexp.MustCompile(`\s+`).ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "( ", "(")
	s = strings.ReplaceAll(s, " )", ")")
	return s
}

func TestSessionPredicates_SchemaMVStaysInSync(t *testing.T) {
	t.Parallel()

	schema, err := os.ReadFile("../../../clickhouse/schema.sql")
	require.NoError(t, err)

	// Scope the check to the chat_session_summaries_mv definition so the
	// fragments are asserted against OUR MV, not a sibling's.
	schemaText := string(schema)
	start := strings.Index(schemaText, "CREATE MATERIALIZED VIEW IF NOT EXISTS chat_session_summaries_mv")
	require.GreaterOrEqual(t, start, 0, "chat_session_summaries_mv not found in schema.sql")
	end := strings.Index(schemaText[start:], "CREATE TABLE")
	require.Positive(t, end, "expected a statement after chat_session_summaries_mv")
	mvSQL := normalizeSQL(schemaText[start : start+end])

	goConstants := normalizeSQL(strings.Join([]string{
		sessionClaudeAPIRequestPredicate,
		sessionClaudeToolResultPredicate,
		sessionAgentUsageRowPredicate,
		sessionAgentToolCallPredicate,
		sessionFailedToolCallPredicate,
		sessionCountedToolCallPredicate,
		sessionToolCallDedupIDExpr,
		sessionCostExpr,
		sessionTotalTokensExpr,
		sessionModelExpr,
	}, "\n"))

	for _, fragment := range sessionSharedPredicateFragments {
		normalized := normalizeSQL(fragment)
		require.Contains(t, mvSQL, normalized,
			"chat_session_summaries_mv drifted from the pinned session predicate fragment — apply the change on every path (Go constants, MV via MODIFY QUERY + backfill, seed)")
		require.Contains(t, goConstants, normalized,
			"session* Go constants drifted from the pinned predicate fragment — apply the change on every path (Go constants, MV via MODIFY QUERY + backfill, seed)")
	}
}

// TestSessionPredicates_SeedBackfillStaysInSync covers the third live copy of
// the session predicates: chatSessionBackfillSQL in the local seed, which
// re-derives chat_session_summaries for pre-cutoff seeded rows. (The prod
// backfill runbook under clickhouse/local/backfill/ is a one-time executed
// artifact and is deliberately not covered — drift after its execution is
// inert.)
func TestSessionPredicates_SeedBackfillStaysInSync(t *testing.T) {
	t.Parallel()

	seed, err := os.ReadFile("../../../../.mise-tasks/seed.mts")
	require.NoError(t, err)

	seedText := string(seed)
	start := strings.Index(seedText, "function chatSessionBackfillSQL")
	require.GreaterOrEqual(t, start, 0, "chatSessionBackfillSQL not found in seed.mts")
	end := strings.Index(seedText[start:], "\nfunction ")
	require.Positive(t, end, "expected a declaration after chatSessionBackfillSQL")
	backfillSQL := normalizeSQL(seedText[start : start+end])

	for _, fragment := range sessionSharedPredicateFragments {
		require.Contains(t, backfillSQL, normalizeSQL(fragment),
			"seed.mts chatSessionBackfillSQL drifted from the pinned session predicate fragment — apply the change on every path (Go constants, MV via MODIFY QUERY + backfill, seed)")
	}
}

// TestSessionSummaryDimensionBindings enforces the summary path's registry
// invariants that the compiler cannot: every filterable dimension must bind
// to a real chat_session_summaries backing — a distinct-values array column
// for scalar/array dimensions, or (for co-located attribution dimensions,
// whose registry key doubles as the tuple field name) a named field of
// attribution_tuples. Without this, a future dimension added with rawExpr
// only would compile, pass narrow-window tests, and emit malformed SQL on
// every window wide enough to route to the summary path.
func TestSessionSummaryDimensionBindings(t *testing.T) {
	t.Parallel()

	schema, err := os.ReadFile("../../../clickhouse/schema.sql")
	require.NoError(t, err)

	schemaText := string(schema)
	start := strings.Index(schemaText, "CREATE TABLE IF NOT EXISTS chat_session_summaries (")
	require.GreaterOrEqual(t, start, 0, "chat_session_summaries not found in schema.sql")
	end := strings.Index(schemaText[start:], "COMMENT ")
	require.Positive(t, end, "expected the chat_session_summaries table to end with a COMMENT")
	tableSQL := schemaText[start : start+end]

	for key, dim := range telemetryDimensionRegistry {
		switch {
		case dim.kind == attributeDimProject:
			require.Equal(t, "gram_project_id", dim.summaryColumn,
				"dimension %q: project dimensions must bind to the gram_project_id key column", key)
		case dim.coLocateSessionFilters:
			require.Empty(t, dim.summaryColumn,
				"dimension %q: co-located dimensions are matched via attribution_tuples, not a summary column", key)
			require.Contains(t, tableSQL, key+" String",
				"dimension %q: registry key must name a field of the attribution_tuples named tuple in chat_session_summaries", key)
		default:
			require.NotEmpty(t, dim.summaryColumn,
				"dimension %q: filterable dimensions need a chat_session_summaries distinct-values column", key)
			require.Contains(t, tableSQL, dim.summaryColumn+" SimpleAggregateFunction(groupUniqArrayArray, Array(String))",
				"dimension %q: summaryColumn %q is not a distinct-values array column of chat_session_summaries", key, dim.summaryColumn)
		}
	}
}

// TestSessionMeasureSelects_PathsAcceptSameSortKeys pins the two ListSessions
// paths to one sort-measure surface: a measure present in only one map would
// make sort_by succeed or fail depending on which side of the routing window
// threshold the request lands.
func TestSessionMeasureSelects_PathsAcceptSameSortKeys(t *testing.T) {
	t.Parallel()

	for key := range sessionMeasureSelects {
		require.Contains(t, sessionSummaryMeasureSelects, key,
			"sort measure %q is raw-path only — wide windows would return 'unknown sort_by measure'", key)
	}
	for key := range sessionSummaryMeasureSelects {
		require.Contains(t, sessionMeasureSelects, key,
			"sort measure %q is summary-path only — narrow windows would return 'unknown sort_by measure'", key)
	}
}
