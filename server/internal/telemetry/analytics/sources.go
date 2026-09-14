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

// dedupedAgentEvents is the innermost scan every event dataset starts from:
// the tenancy and window filter, then LIMIT 1 BY record_id so a redelivered
// record counts once before anything is aggregated. No aggregate function can
// express that, which is why it lives here and not in a measure.
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
	return builder.Suffix("LIMIT 1 BY organization_id, project_id, record_id")
}

// sessionsSource collapses agent_events to one row per session. Dimensions
// resolve to the latest observed value; the measures are identity-aware
// counts, so a caller composing sum(turn_count) gets the de-duplicated figure
// without needing to know why.
func sessionsSource(scope Scope) squirrel.SelectBuilder {
	return sq.Select(
		"organization_id",
		"project_id",
		"session_id",
		"min(occurred_at_unix_nano) AS started_at",
		"max(occurred_at_unix_nano) AS ended_at",
		"argMax(user_email, observed_at_unix_nano) AS user_email",
		"argMax(model, observed_at_unix_nano) AS model",
		"argMax(surface, observed_at_unix_nano) AS surface",
		"argMax(provider, observed_at_unix_nano) AS provider",
		"uniqExactIf(turn_id, turn_id != '') AS turn_count",
		"uniqExactIf(event_id, event_type LIKE 'tool_call%') AS tool_call_count",
	).
		FromSelect(dedupedAgentEvents(scope, squirrel.NotEq{"session_id": ""}), "deduped").
		GroupBy("organization_id", "project_id", "session_id")
}

// toolCallsSource collapses agent_events to one row per tool call. Pre- and
// post-observations of one call are separate rows by design (the canonical
// event type is part of identity), so argMax by observation time resolves to
// the terminal observation. A call that was blocked and never completed keeps
// its pre-observation, which is the point.
func toolCallsSource(scope Scope) squirrel.SelectBuilder {
	return sq.Select(
		"organization_id",
		"project_id",
		"event_id AS tool_call_id",
		"argMax(tool_name, observed_at_unix_nano) AS tool_name",
		"argMax(session_id, observed_at_unix_nano) AS session_id",
		"argMax(user_email, observed_at_unix_nano) AS user_email",
		"argMax(surface, observed_at_unix_nano) AS surface",
		"argMax(outcome, observed_at_unix_nano) AS status",
		"argMax(duration_nano, observed_at_unix_nano) AS duration_nano",
		"min(occurred_at_unix_nano) AS started_at",
		"max(occurred_at_unix_nano) AS ended_at",
	).
		FromSelect(dedupedAgentEvents(scope,
			squirrel.Like{"event_type": "tool_call%"},
			squirrel.NotEq{"event_id": ""},
		), "deduped").
		GroupBy("organization_id", "project_id", "event_id")
}
