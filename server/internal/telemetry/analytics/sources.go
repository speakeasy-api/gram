package analytics

import (
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

// QueryContext is what one query runs in: its tenant, window and loaded
// lookups.
type QueryContext struct {
	Tenant  Tenant
	Window  Window
	Lookups LookupMaps
}

// toolCallEventTypes are the observations of one tool call: the call itself
// (semantic-convention and Codex producers name it so), its result, and the
// decision that admitted or blocked it. A blocked call is a decision alone,
// and it is still a call, so every predicate over tool calls admits all
// three.
var toolCallEventTypes = []string{"tool_call", "tool_call_result", "tool_decision"}

// Source declares one row per Key of a dataset: the table it reads, the
// predicates, and the Columns that fold or merge into that row. Observations
// is the predicates alone over agent_events, for listing a dimension's values.
type Source struct {
	Table   engine
	Where   []squirrel.Sqlizer
	Key     string
	Columns []string
	// Having keeps a folded row. On a source a Typeahead field lists values
	// from, it must name columns the fold keeps any non-empty value of, so
	// Observations can test it on the raw rows.
	Having string
}

func (s Source) Rows(qc QueryContext) squirrel.SelectBuilder {
	builder := s.Table.rows(qc, s)
	if s.Having != "" {
		builder = builder.Having(s.Having)
	}
	return builder
}

// Observations reads the predicates over the plain agent_events scan, so a
// value a later correction withdrew can still show up.
func (s Source) Observations(qc QueryContext) squirrel.SelectBuilder {
	builder := where(agentEvents.scan(qc), s.Where)
	if s.Having != "" {
		keys := where(agentEvents.scan(qc).RemoveColumns().Column(s.Key), s.Where).Where(s.Having)
		builder = builder.Where(squirrel.Expr(s.Key+" IN (?)", keys))
	}
	return builder
}

// engine reads a table the way its ClickHouse engine family merges it.
type engine interface {
	rows(qc QueryContext, s Source) squirrel.SelectBuilder
	// planSuffix names how a query is answered in its plan name, a stable
	// diagnostic identifier rather than a table a client could depend on.
	planSuffix() string
	// retentionDays is how long the table keeps a row, from its TTL in
	// server/clickhouse/schema.sql. A window reaching past it reads rows the
	// TTL has already dropped and comes back silently incomplete.
	retentionDays() int
}

// mergeTree rows are facts as appended. Its fold must be idempotent, so a
// redelivered row changes nothing: argMax, min, max, uniqExact, never count
// or sum over raw rows.
type mergeTree struct {
	table, time, plan string
	retention         int
}

// aggregatingMergeTree rows are partial states per key until merges catch
// up, so keys are picked by start and every row of those keys is merged.
type aggregatingMergeTree struct {
	table, start, plan string
	retention          int
}

var (
	agentEvents = mergeTree{table: "agent_events", time: "occurred_at_unix_nano", plan: "agent_events", retention: MaxEventTimeRangeDays}
	// The tool_calls plan name predates the move off agent_events and stays,
	// being a stable identifier.
	toolCalls = aggregatingMergeTree{table: "tool_calls", start: "started_at_unix_nano", plan: "agent_events", retention: MaxEventTimeRangeDays}
	// riskFindingStates is one row per finding, each column the winning
	// copy's value, folded by risk_finding_states_mv with the shadow engine's
	// comparison rows set aside.
	riskFindingStates = aggregatingMergeTree{table: "risk_finding_states", start: "created_at_unix_nano", plan: "risk_findings", retention: MaxEventTimeRangeDays}
)

func (t mergeTree) planSuffix() string            { return t.plan }
func (t aggregatingMergeTree) planSuffix() string { return t.plan }
func (t mergeTree) retentionDays() int            { return t.retention }
func (t aggregatingMergeTree) retentionDays() int { return t.retention }

func (t mergeTree) scan(qc QueryContext) squirrel.SelectBuilder {
	return sq.Select("*").
		From(t.table).
		Where(tenant(qc)).
		Where(t.time+" >= ?", qc.Window.FromUnixNano).
		Where(t.time+" < ?", qc.Window.ToUnixNano)
}

func (t mergeTree) rows(qc QueryContext, s Source) squirrel.SelectBuilder {
	return where(t.scan(qc).RemoveColumns().Columns(s.Columns...), s.Where).
		GroupBy("organization_id", "project_id", s.Key)
}

func (t aggregatingMergeTree) rows(qc QueryContext, s Source) squirrel.SelectBuilder {
	keys := sq.Select(s.Key).
		From(t.table).
		Where(tenant(qc)).
		Where(t.start+" >= ?", qc.Window.FromUnixNano).
		Where(t.start+" < ?", qc.Window.ToUnixNano)
	return sq.Select(s.Columns...).
		From(t.table).
		Where(tenant(qc)).
		Where(squirrel.Expr(s.Key+" IN (?)", keys)).
		GroupBy("organization_id", "project_id", s.Key).
		Having("min("+t.start+") >= ?", qc.Window.FromUnixNano).
		Having("min("+t.start+") < ?", qc.Window.ToUnixNano)
}

func tenant(qc QueryContext) squirrel.Eq {
	return squirrel.Eq{"organization_id": qc.Tenant.OrganizationID, "project_id": qc.Tenant.ProjectID}
}

func where(builder squirrel.SelectBuilder, conditions []squirrel.Sqlizer) squirrel.SelectBuilder {
	for _, condition := range conditions {
		builder = builder.Where(condition)
	}
	return builder
}

// toolCallsSource is one row per tool call (decision, call, result), Skill
// invocations included. A blocked call has only its decision, so that row is
// the call and its status is rejected.
var toolCallsSource = Source{
	Where: []squirrel.Sqlizer{
		squirrel.Eq{"event_type": toolCallEventTypes},
		squirrel.NotEq{"event_id": ""},
	},
	Key:   "event_id",
	Table: toolCalls,
	Columns: []string{
		"organization_id",
		"project_id",
		"event_id AS tool_call_id",
		"argMaxIfMerge(tool_name) AS tool_name",
		"argMaxIfMerge(mcp_server_name) AS mcp_server_name",
		"argMaxIfMerge(mcp_tool_name) AS mcp_tool_name",
		"argMaxIfMerge(skill_name) AS skill_name",
		"argMaxIfMerge(session_id) AS session_id",
		"argMaxIfMerge(user_email) AS user_email",
		"argMaxIfMerge(surface) AS surface",
		"argMaxIfMerge(status) AS status",
		"argMaxIfMerge(duration_nano) AS duration_nano",
		"min(started_at_unix_nano) AS started_at",
		"max(ended_at_unix_nano) AS ended_at",
	},
	Having: "",
}

// skillsSource is one row per tool call that named a skill. Having runs after
// the fold because not every observation of a call carries the name: a
// blocked invocation's decision does not, its result does.
var skillsSource = Source{
	Table:   toolCallsSource.Table,
	Where:   toolCallsSource.Where,
	Key:     toolCallsSource.Key,
	Columns: toolCallsSource.Columns,
	Having:  "skill_name != ''",
}

// riskFindingsSource is one row per live finding: not suppressed, not marked
// a false positive and not a could-not-analyze sentinel. Its table keeps no
// matched value, redacted or not, and no chat or message id, so nothing a
// dataset declares over it can name what was found or where.
var riskFindingsSource = Source{
	Table: riskFindingStates,
	Where: nil,
	Key:   "id",
	Columns: []string{
		"organization_id",
		"project_id",
		"id",
		"min(created_at_unix_nano) AS detected_at",
		"argMaxIfMerge(category) AS category",
		"argMaxIfMerge(rule_id) AS rule_id",
		"argMaxIfMerge(source) AS source",
		"argMaxIfMerge(risk_policy_id) AS risk_policy_id",
		"argMaxIfMerge(chat_source) AS chat_source",
		"argMaxIfMerge(team) AS team",
		// Neither email column is aliased, so both still name the state.
		"if(argMaxIfMerge(user_email) != '', argMaxIfMerge(user_email), argMaxIfMerge(external_user_id)) AS user_label",
		"argMaxIfMerge(enforcement_outcome) AS enforcement_outcome",
	},
	Having: "argMaxMerge(live) = 1",
}
