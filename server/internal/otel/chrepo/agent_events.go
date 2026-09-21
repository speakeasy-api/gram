package chrepo

import (
	"context"
	"fmt"
)

// AgentEventRow is one row of the agent_events table: one observed agent
// occurrence, in agent vocabulary. Column order follows the DDL in
// clickhouse/schema.sql. Empty strings and zero numbers mean the producer did
// not state the value; nothing here is ever invented.
type AgentEventRow struct {
	OrganizationID     string `ch:"organization_id"`
	ProjectID          string `ch:"project_id"`
	OccurredAtUnixNano int64  `ch:"occurred_at_unix_nano"`
	ObservedAtUnixNano int64  `ch:"observed_at_unix_nano"`

	RecordID  string `ch:"record_id"`
	SessionID string `ch:"session_id"`
	TurnID    string `ch:"turn_id"`

	EventID      string `ch:"event_id"`
	EventType    string `ch:"event_type"`
	RawEventName string `ch:"raw_event_name"`

	Source   string `ch:"source"`
	Provider string `ch:"provider"`
	Surface  string `ch:"surface"`

	UserID         string `ch:"user_id"`
	UserEmail      string `ch:"user_email"`
	ExternalUserID string `ch:"external_user_id"`

	AccountType   string `ch:"account_type"`
	BillingMode   string `ch:"billing_mode"`
	ExternalOrgID string `ch:"external_org_id"`
	DeviceID      string `ch:"device_id"`

	DepartmentName string   `ch:"department_name"`
	DivisionName   string   `ch:"division_name"`
	JobTitle       string   `ch:"job_title"`
	EmployeeType   string   `ch:"employee_type"`
	CostCenterName string   `ch:"cost_center_name"`
	Roles          []string `ch:"roles"`
	Groups         []string `ch:"groups"`

	Model         string `ch:"model"`
	QuerySource   string `ch:"query_source"`
	SkillName     string `ch:"skill_name"`
	AgentName     string `ch:"agent_name"`
	MCPServerName string `ch:"mcp_server_name"`
	MCPToolName   string `ch:"mcp_tool_name"`
	ToolName      string `ch:"tool_name"`

	Text string `ch:"text"`

	Outcome        string `ch:"outcome"`
	OutcomeMessage string `ch:"outcome_message"`
	DurationNano   int64  `ch:"duration_nano"`

	InputContent  string `ch:"input_content"`
	OutputContent string `ch:"output_content"`

	InputTokens      int64   `ch:"input_tokens"`
	OutputTokens     int64   `ch:"output_tokens"`
	CacheReadTokens  int64   `ch:"cache_read_tokens"`
	CacheWriteTokens int64   `ch:"cache_write_tokens"`
	CostUSD          float64 `ch:"cost_usd"`

	Attributes         string `ch:"attributes"`
	ResourceAttributes string `ch:"resource_attributes"`
	ScopeAttributes    string `ch:"scope_attributes"`
}

// agentEventColumns is the agent_events column list, in the exact order
// InsertAgentEvents binds values.
var agentEventColumns = []string{
	"organization_id",
	"project_id",
	"occurred_at_unix_nano",
	"observed_at_unix_nano",
	"record_id",
	"session_id",
	"turn_id",
	"event_id",
	"event_type",
	"raw_event_name",
	"source",
	"provider",
	"surface",
	"user_id",
	"user_email",
	"external_user_id",
	"account_type",
	"billing_mode",
	"external_org_id",
	"device_id",
	"department_name",
	"division_name",
	"job_title",
	"employee_type",
	"cost_center_name",
	"roles",
	"groups",
	"model",
	"query_source",
	"skill_name",
	"agent_name",
	"mcp_server_name",
	"mcp_tool_name",
	"tool_name",
	"text",
	"outcome",
	"outcome_message",
	"duration_nano",
	"input_content",
	"output_content",
	"input_tokens",
	"output_tokens",
	"cache_read_tokens",
	"cache_write_tokens",
	"cost_usd",
	"attributes",
	"resource_attributes",
	"scope_attributes",
}

// InsertAgentEvents writes a batch of rows to agent_events. The table is
// append-only, so a redelivered batch lands as duplicate rows that readers
// collapse on record_id.
func (q *Queries) InsertAgentEvents(ctx context.Context, rows []AgentEventRow) error {
	if len(rows) == 0 {
		return nil
	}

	builder := sq.Insert("agent_events").Columns(agentEventColumns...)
	for _, row := range rows {
		builder = builder.Values(
			row.OrganizationID,
			row.ProjectID,
			row.OccurredAtUnixNano,
			row.ObservedAtUnixNano,
			row.RecordID,
			row.SessionID,
			row.TurnID,
			row.EventID,
			row.EventType,
			row.RawEventName,
			row.Source,
			row.Provider,
			row.Surface,
			row.UserID,
			row.UserEmail,
			row.ExternalUserID,
			row.AccountType,
			row.BillingMode,
			row.ExternalOrgID,
			row.DeviceID,
			row.DepartmentName,
			row.DivisionName,
			row.JobTitle,
			row.EmployeeType,
			row.CostCenterName,
			stringsOrEmpty(row.Roles),
			stringsOrEmpty(row.Groups),
			row.Model,
			row.QuerySource,
			row.SkillName,
			row.AgentName,
			row.MCPServerName,
			row.MCPToolName,
			row.ToolName,
			row.Text,
			row.Outcome,
			row.OutcomeMessage,
			row.DurationNano,
			row.InputContent,
			row.OutputContent,
			row.InputTokens,
			row.OutputTokens,
			row.CacheReadTokens,
			row.CacheWriteTokens,
			row.CostUSD,
			row.Attributes,
			row.ResourceAttributes,
			row.ScopeAttributes,
		)
	}

	query, args, err := builder.ToSql()
	if err != nil {
		return fmt.Errorf("build agent_events insert query: %w", err)
	}

	if err := q.conn.Exec(chWriterInsertContext(ctx), query, args...); err != nil {
		return fmt.Errorf("insert agent_events: %w", err)
	}
	return nil
}

// stringsOrEmpty binds a nil slice as an empty ClickHouse array rather than
// a NULL the Array(String) column would reject.
func stringsOrEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
