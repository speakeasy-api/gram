package analytics

import (
	"github.com/Masterminds/squirrel"

	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
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
	// Having keeps a folded row. It must name columns the fold keeps any
	// non-empty value of, so Observations can test it on the raw rows.
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
}

// mergeTree rows are facts as appended. Its fold must be idempotent, so a
// redelivered row changes nothing: argMax, min, max, uniqExact, never count
// or sum over raw rows.
type mergeTree struct{ table, time string }

// aggregatingMergeTree rows are partial states per key until merges catch
// up, so keys are picked by start and every row of those keys is merged.
type aggregatingMergeTree struct{ table, start string }

// latestCopy rows are an event log of copies per key, where a later copy
// records a change of state, so each key reads as its winning copy under
// order. shared are predicates every copy of a key agrees on, which may prune
// the scan before the dedup; Source.Where applies to the winning copy, since
// it can test state a later copy changed.
type latestCopy struct {
	table, time, order string
	shared             []squirrel.Sqlizer
}

var (
	agentEvents = mergeTree{table: "agent_events", time: "occurred_at_unix_nano"}
	toolCalls   = aggregatingMergeTree{table: "tool_calls", start: "started_at_unix_nano"}
	// riskFindings is the risk scanners' findings log, read the way every
	// risk surface reads it: the winning copy per finding, with the shadow
	// engine's comparison rows set aside.
	riskFindings = latestCopy{
		table:  "risk_findings",
		time:   "created_at",
		order:  chrepo.LatestCopyOrderSQL,
		shared: []squirrel.Sqlizer{squirrel.Expr(chrepo.NotShadowCond)},
	}
)

// The plan names of the agent_events datasets predate their move onto the
// tool_calls table and stay as they were, being stable identifiers.
func (mergeTree) planSuffix() string            { return "agent_events" }
func (aggregatingMergeTree) planSuffix() string { return "agent_events" }
func (t latestCopy) planSuffix() string         { return t.table }

func (t latestCopy) rows(qc QueryContext, s Source) squirrel.SelectBuilder {
	copies := sq.Select("*", "ROW_NUMBER() OVER (PARTITION BY "+s.Key+" ORDER BY "+t.order+") AS copy_rank").
		From(t.table).
		Where(tenant(qc)).
		Where(t.time+" >= fromUnixTimestamp64Nano(toInt64(?), 'UTC')", qc.Window.FromUnixNano).
		Where(t.time+" < fromUnixTimestamp64Nano(toInt64(?), 'UTC')", qc.Window.ToUnixNano)
	copies = where(copies, t.shared)
	return where(sq.Select(s.Columns...).FromSelect(copies, "copies").Where("copy_rank = 1"), s.Where)
}

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
		Having("started_at >= ?", qc.Window.FromUnixNano).
		Having("started_at < ?", qc.Window.ToUnixNano)
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
// a false positive and not a could-not-analyze sentinel. It reads no matched
// value, redacted or not, and no chat or message id, so nothing a dataset
// declares over it can name what was found or where.
var riskFindingsSource = Source{
	Table: riskFindings,
	Where: []squirrel.Sqlizer{
		squirrel.Expr(chrepo.LiveStateCond),
		squirrel.Expr(chrepo.NotDeadLetterCond),
	},
	Key: "id",
	Columns: []string{
		"organization_id",
		"project_id",
		"id",
		"toUnixTimestamp64Nano(created_at) AS detected_at",
		"category",
		"rule_id",
		"source",
		"risk_policy_id",
		"chat_source",
		"team",
		"if(user_email != '', user_email, external_user_id) AS user_label",
		"toString(enforcement_outcome) AS enforcement_outcome",
	},
	Having: "",
}
