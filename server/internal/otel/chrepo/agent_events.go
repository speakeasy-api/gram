package chrepo

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
