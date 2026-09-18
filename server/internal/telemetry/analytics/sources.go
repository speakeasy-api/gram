package analytics

import "github.com/Masterminds/squirrel"

// sq is the squirrel builder pre-configured for ClickHouse (? placeholders).
var sq = squirrel.StatementBuilder.PlaceholderFormat(squirrel.Question)

// Scope is the one tenancy and time window a query runs in. Organization
// comes from the authenticated session, project from the request, and both
// are stamped on every row at the ingest edge, so they bound the scan before
// anything is collapsed.
type Scope struct {
	OrganizationID string
	ProjectID      string
	FromUnixNano   int64
	ToUnixNano     int64
}

// SourceQuery builds the query that turns observations into rows at the
// dataset's grain. It is a subquery over the dataset's table, run as part of
// every request; nothing is stored.
type SourceQuery func(scope Scope) squirrel.SelectBuilder

// toolCallEventTypes are the observations of one tool call: the call itself
// (semantic-convention and Codex producers name it so), its result, and the
// decision that admitted or blocked it. A blocked call is a decision alone,
// and it is still a call, so every predicate over tool calls admits all
// three. The two spellings below must stay in step.
var toolCallEventTypes = []string{"tool_call", "tool_call_result", "tool_decision"}

const toolCallEventTypesSQL = "event_type IN ('tool_call', 'tool_call_result', 'tool_decision')"

// dedupedAgentEvents is the innermost scan every event dataset starts from:
// the tenancy and window filter, then LIMIT 1 BY record_id so a redelivered
// record counts once before anything is aggregated. No aggregate function can
// express that, which is why it lives here and not in a measure.
//
// The rows are ordered by observation time first. LIMIT BY keeps whichever
// row it meets first, and without an order that is whichever row ClickHouse
// happened to read first, so a record re-emitted with a correction could
// survive as its stale copy. Newest observed wins, which is also the rule
// every argMax downstream applies.
func dedupedAgentEvents(scope Scope, extra ...squirrel.Sqlizer) squirrel.SelectBuilder {
	builder := sq.Select("*").
		From("agent_events").
		Where(squirrel.Eq{"organization_id": scope.OrganizationID}).
		Where(squirrel.Eq{"project_id": scope.ProjectID}).
		Where("occurred_at_unix_nano >= ?", scope.FromUnixNano).
		Where("occurred_at_unix_nano < ?", scope.ToUnixNano)
	for _, condition := range extra {
		builder = builder.Where(condition)
	}
	return builder.
		OrderBy("observed_at_unix_nano DESC").
		Suffix("LIMIT 1 BY organization_id, project_id, record_id")
}

// sessionsSource collapses agent_events to one row per session. Dimensions
// resolve to the latest observed value that stated one: a session usually ends
// on a hook or MCP event that carries no model, and that must not blank the
// model an API request stated. The measures are identity-aware counts, so a
// caller composing sum(turn_count) gets the de-duplicated figure without
// needing to know why.
func sessionsSource(scope Scope) squirrel.SelectBuilder {
	return sq.Select(
		"organization_id",
		"project_id",
		"session_id",
		"min(occurred_at_unix_nano) AS started_at",
		"max(occurred_at_unix_nano) AS ended_at",
		"argMaxIf(user_email, observed_at_unix_nano, user_email != '') AS user_email",
		"argMaxIf(model, observed_at_unix_nano, model != '') AS model",
		"argMaxIf(surface, observed_at_unix_nano, surface != '') AS surface",
		"argMaxIf(provider, observed_at_unix_nano, provider != '') AS provider",
		"uniqExactIf(turn_id, turn_id != '') AS turn_count",
		"uniqExactIf(event_id, "+toolCallEventTypesSQL+") AS tool_call_count",
	).
		FromSelect(dedupedAgentEvents(scope, squirrel.NotEq{"session_id": ""}), "deduped").
		GroupBy("organization_id", "project_id", "session_id")
}

// toolCallsSource collapses agent_events to one row per tool call. Pre- and
// post-observations of one call are separate rows by design (the canonical
// event type is part of identity), so argMax by observation time resolves to
// the terminal observation. A call that was blocked never ran, so its only
// observation is the decision that blocked it: that row alone is the call,
// and its outcome resolves to rejected.
func toolCallsSource(scope Scope) squirrel.SelectBuilder {
	return sq.Select(
		"organization_id",
		"project_id",
		"event_id AS tool_call_id",
		"argMaxIf(tool_name, observed_at_unix_nano, tool_name != '') AS tool_name",
		"argMaxIf(mcp_server_name, observed_at_unix_nano, mcp_server_name != '') AS mcp_server_name",
		"argMaxIf(mcp_tool_name, observed_at_unix_nano, mcp_tool_name != '') AS mcp_tool_name",
		"argMaxIf(session_id, observed_at_unix_nano, session_id != '') AS session_id",
		"argMaxIf(user_email, observed_at_unix_nano, user_email != '') AS user_email",
		"argMaxIf(surface, observed_at_unix_nano, surface != '') AS surface",
		"argMaxIf(outcome, observed_at_unix_nano, outcome != '') AS status",
		"argMaxIf(duration_nano, observed_at_unix_nano, duration_nano != 0) AS duration_nano",
		"min(occurred_at_unix_nano) AS started_at",
		"max(occurred_at_unix_nano) AS ended_at",
	).
		FromSelect(dedupedAgentEvents(scope,
			squirrel.Eq{"event_type": toolCallEventTypes},
			squirrel.NotEq{"event_id": ""},
		), "deduped").
		GroupBy("organization_id", "project_id", "event_id")
}
