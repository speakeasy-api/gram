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
	require.Positive(t, start, "chat_session_summaries_mv not found in schema.sql")
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
