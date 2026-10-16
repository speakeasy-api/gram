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

// Source declares how a dataset reads agent_events: the dataset's predicates,
// then one row per Key folded from Columns. Rows is that fold, for queries.
// Observations is the predicates alone, for listing a dimension's values.
type Source struct {
	// Table, when set, already holds one row of aggregate states per Key,
	// filled from agent_events as records arrive, and Columns merge those
	// states. Rows then reads it instead of collapsing and folding the window.
	Table   string
	Where   []squirrel.Sqlizer
	Key     string
	Columns []string
	// Having keeps a folded row. It must name columns the fold keeps any
	// non-empty value of, so Observations can test it on the raw rows.
	Having string
}

// Rows collapses redelivered records, applies the predicates, then folds.
// The predicates run after the collapse: a correction can move a record out
// of the dataset, and filtering first would leave its stale copy to win.
func (s Source) Rows(qc QueryContext) squirrel.SelectBuilder {
	if s.Table != "" {
		return s.merged(qc)
	}
	builder := sq.Select(s.Columns...).
		FromSelect(where(collapsedAgentEvents(qc), s.Where), "observed").
		GroupBy("organization_id", "project_id", s.Key)
	if s.Having != "" {
		builder = builder.Having(s.Having)
	}
	return builder
}

// merged reads one row per Key from the source's table. The call's start
// decides whether it falls inside the window.
func (s Source) merged(qc QueryContext) squirrel.SelectBuilder {
	builder := sq.Select(s.Columns...).
		From(s.Table).
		Where(squirrel.Eq{"organization_id": qc.Tenant.OrganizationID}).
		Where(squirrel.Eq{"project_id": qc.Tenant.ProjectID}).
		GroupBy("organization_id", "project_id", s.Key).
		Having("started_at >= ?", qc.Window.FromUnixNano).
		Having("started_at < ?", qc.Window.ToUnixNano)
	if s.Having != "" {
		builder = builder.Having(s.Having)
	}
	return builder
}

// Observations reads the predicates over the plain scan. It skips the
// collapse, which sorts the whole window in memory, so a value a later
// correction withdrew can still show up.
func (s Source) Observations(qc QueryContext) squirrel.SelectBuilder {
	builder := where(scopedAgentEvents(qc), s.Where)
	if s.Having != "" {
		keys := where(scopedAgentEvents(qc).RemoveColumns().Column(s.Key), s.Where).Where(s.Having)
		builder = builder.Where(squirrel.Expr(s.Key+" IN (?)", keys))
	}
	return builder
}

func scopedAgentEvents(qc QueryContext) squirrel.SelectBuilder {
	return sq.Select("*").
		From("agent_events").
		Where(squirrel.Eq{"organization_id": qc.Tenant.OrganizationID}).
		Where(squirrel.Eq{"project_id": qc.Tenant.ProjectID}).
		Where("occurred_at_unix_nano >= ?", qc.Window.FromUnixNano).
		Where("occurred_at_unix_nano < ?", qc.Window.ToUnixNano)
}

// collapsedAgentEvents keeps the newest observed copy of each record. The
// sort holds the whole window in memory and cannot spill, so it is the
// costliest step of any query.
func collapsedAgentEvents(qc QueryContext) squirrel.SelectBuilder {
	scoped := scopedAgentEvents(qc).
		OrderBy("observed_at_unix_nano DESC").
		Suffix("LIMIT 1 BY organization_id, project_id, record_id")
	return sq.Select("*").FromSelect(scoped, "scoped")
}

func where(builder squirrel.SelectBuilder, conditions []squirrel.Sqlizer) squirrel.SelectBuilder {
	for _, condition := range conditions {
		builder = builder.Where(condition)
	}
	return builder
}

// sessionsSource is one row per session. Dimensions resolve to the latest
// observed value that stated one: a session usually ends on a hook or MCP
// event that carries no model, and that must not blank the model an API
// request stated.
var sessionsSource = Source{
	Table: "",
	Where: []squirrel.Sqlizer{squirrel.NotEq{"session_id": ""}},
	Key:   "session_id",
	Columns: []string{
		"organization_id",
		"project_id",
		"session_id",
		"min(occurred_at_unix_nano) AS started_at",
		"max(occurred_at_unix_nano) AS ended_at",
		"argMaxIf(user_email, observed_at_unix_nano, user_email != '') AS user_email",
		"argMaxIf(model, observed_at_unix_nano, model != '') AS model",
		"argMaxIf(surface, observed_at_unix_nano, surface != '') AS surface",
		"argMaxIf(provider, observed_at_unix_nano, provider != '') AS provider",
		// uniqExact yields UInt64; the catalog declares these Int64.
		"toInt64(uniqExactIf(turn_id, turn_id != '')) AS turn_count",
		"toInt64(uniqExactIf(event_id, " + toolCallEventTypesSQL + ")) AS tool_call_count",
	},
	Having: "",
}

// toolCallsSource is one row per tool call (decision, call, result), Skill
// invocations included, read from the tool_calls table that tool_calls_mv
// fills. A blocked call has only its decision, so that row is the call and
// its status is rejected.
var toolCallsSource = Source{
	Where: []squirrel.Sqlizer{
		squirrel.Eq{"event_type": toolCallEventTypes},
		squirrel.NotEq{"event_id": ""},
	},
	Key:   "event_id",
	Table: "tool_calls",
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
