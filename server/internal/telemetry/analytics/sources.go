package analytics

import (
	"strings"

	"github.com/Masterminds/squirrel"
)

// sq is the squirrel builder pre-configured for ClickHouse (? placeholders).
var sq = squirrel.StatementBuilder.PlaceholderFormat(squirrel.Question)

// Tenant is whose rows a query reads. Organization comes from the
// authenticated session, project from the request, and both are stamped on
// every row at the ingest edge, so they bound the scan before anything is
// collapsed.
type Tenant struct {
	OrganizationID string
	ProjectID      string
}

// Window is the time range a query reads: from inclusive, to exclusive, in
// Unix nanoseconds.
type Window struct {
	FromUnixNano int64
	ToUnixNano   int64
}

// QueryContext is what one query runs in: the tenant whose rows it reads and
// the time window. It is not called a scope, because that word is
// authorization vocabulary here.
type QueryContext struct {
	Tenant Tenant
	Window Window
}

// SourceQuery builds the query that turns observations into rows at the
// dataset's grain. It is a subquery over the dataset's table, run as part of
// every request; nothing is stored.
type SourceQuery func(qc QueryContext) squirrel.SelectBuilder

// toolCallEventTypes are the observations of one tool call: the call itself
// (semantic-convention and Codex producers name it so), its result, and the
// decision that admitted or blocked it. A blocked call is a decision alone,
// and it is still a call, so every predicate over tool calls admits all
// three.
var toolCallEventTypes = []string{"tool_call", "tool_call_result", "tool_decision"}

// toolCallEventTypesSQL is the same set as a SQL predicate, for the one place
// squirrel cannot bind it: a condition inside an aggregate in a select list.
// Derived from the slice above so the two datasets cannot disagree; the
// values are those declared constants, never input.
var toolCallEventTypesSQL = "event_type IN (" + quotedList(toolCallEventTypes) + ")"

// quotedList renders declared string constants as a SQL list.
func quotedList(values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = "'" + value + "'"
	}
	return strings.Join(quoted, ", ")
}

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
//
// The collapse runs over the complete scoped record, and only then do the
// dataset's own predicates apply. Filtering first would let a correction
// that moved a record out of the dataset (a session id withdrawn, an event
// retyped) be dropped before the collapse, leaving its stale copy to win.
func dedupedAgentEvents(qc QueryContext, extra ...squirrel.Sqlizer) squirrel.SelectBuilder {
	scoped := sq.Select("*").
		From("agent_events").
		Where(squirrel.Eq{"organization_id": qc.Tenant.OrganizationID}).
		Where(squirrel.Eq{"project_id": qc.Tenant.ProjectID}).
		Where("occurred_at_unix_nano >= ?", qc.Window.FromUnixNano).
		Where("occurred_at_unix_nano < ?", qc.Window.ToUnixNano).
		OrderBy("observed_at_unix_nano DESC").
		Suffix("LIMIT 1 BY organization_id, project_id, record_id")
	builder := sq.Select("*").FromSelect(scoped, "scoped")
	for _, condition := range extra {
		builder = builder.Where(condition)
	}
	return builder
}

// sessionsSource collapses agent_events to one row per session. Dimensions
// resolve to the latest observed value that stated one: a session usually ends
// on a hook or MCP event that carries no model, and that must not blank the
// model an API request stated. The measures are identity-aware counts, so a
// caller composing sum(turn_count) gets the de-duplicated figure without
// needing to know why.
func sessionsSource(qc QueryContext) squirrel.SelectBuilder {
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
		// uniqExact yields UInt64; the catalog declares these Int64, and the
		// column must be what the contract says it is.
		"toInt64(uniqExactIf(turn_id, turn_id != '')) AS turn_count",
		"toInt64(uniqExactIf(event_id, "+toolCallEventTypesSQL+")) AS tool_call_count",
	).
		FromSelect(dedupedAgentEvents(qc, squirrel.NotEq{"session_id": ""}), "deduped").
		GroupBy("organization_id", "project_id", "session_id")
}

// toolCallsSource is one row per tool call, Skill invocations included.
func toolCallsSource(qc QueryContext) squirrel.SelectBuilder {
	return collapsedToolCalls(qc)
}

// skillsSource is one row per tool call that named a skill. The filter runs
// after the collapse because not every observation of a call carries the
// name: a blocked invocation's decision does not, its result does.
func skillsSource(qc QueryContext) squirrel.SelectBuilder {
	return collapsedToolCalls(qc).Having("skill_name != ''")
}

// collapsedToolCalls folds the observations of each tool call (decision,
// call, result) into one row at its latest observation. A blocked call has
// only its decision, so that row is the call and its status is rejected.
func collapsedToolCalls(qc QueryContext) squirrel.SelectBuilder {
	return sq.Select(
		"organization_id",
		"project_id",
		"event_id AS tool_call_id",
		"argMaxIf(tool_name, observed_at_unix_nano, tool_name != '') AS tool_name",
		"argMaxIf(mcp_server_name, observed_at_unix_nano, mcp_server_name != '') AS mcp_server_name",
		"argMaxIf(mcp_tool_name, observed_at_unix_nano, mcp_tool_name != '') AS mcp_tool_name",
		"argMaxIf(skill_name, observed_at_unix_nano, skill_name != '') AS skill_name",
		"argMaxIf(session_id, observed_at_unix_nano, session_id != '') AS session_id",
		"argMaxIf(user_email, observed_at_unix_nano, user_email != '') AS user_email",
		"argMaxIf(surface, observed_at_unix_nano, surface != '') AS surface",
		"argMaxIf(outcome, observed_at_unix_nano, outcome != '') AS status",
		"argMaxIf(duration_nano, observed_at_unix_nano, duration_nano != 0) AS duration_nano",
		"min(occurred_at_unix_nano) AS started_at",
		"max(occurred_at_unix_nano) AS ended_at",
	).
		FromSelect(dedupedAgentEvents(qc,
			squirrel.Eq{"event_type": toolCallEventTypes},
			squirrel.NotEq{"event_id": ""},
		), "deduped").
		GroupBy("organization_id", "project_id", "event_id")
}
