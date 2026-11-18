// Package mocktasks is the tool catalog of the local mock MCP server
// (server/cmd/mock-mcp), a fictional task tracker. The local seed fixtures
// read the same catalog, so the remote server they register, its stored tool
// metadata and its seeded telemetry name exactly the tools the mock lists.
//
// The catalog covers each state of the dashboard Inspect tab: read-only and
// destructive tools, a slow tool, a tool that returns an error result, a long
// name with a long multi-line description, nested/array/enum parameters, and
// tools without parameters.
package mocktasks

// Tool is one tool of the mock server.
type Tool struct {
	Name        string
	Title       string
	Description string
	ReadOnly    bool
	Destructive bool
	Idempotent  bool
	OpenWorld   bool
	// InputSchema is the tool's JSON Schema.
	InputSchema string
	// Slow makes the mock wait before answering, like a report job.
	Slow bool
	// Fails makes the mock answer with an MCP error result (isError).
	Fails bool
}

// UnlistedTool is stored in the seeded tool metadata but missing from the
// mock's tools/list, so the Inspect tab shows metadata drift for it.
const UnlistedTool = "bulk_reassign_tasks"

const noParams = `{"type":"object","properties":{}}`

// Tools is the catalog the mock lists, in listing order.
var Tools = []Tool{
	{
		Name:        "search_tasks",
		Title:       "Search tasks",
		Description: "Search tasks by text, status, assignee and label.",
		ReadOnly:    true, Destructive: false, Idempotent: true, OpenWorld: false,
		InputSchema: `{"type":"object","required":["query"],"properties":{
  "query":{"type":"string","description":"Words to match in the title or description."},
  "filters":{"type":"object","description":"Optional narrowing.","properties":{
    "status":{"type":"string","enum":["open","in_progress","blocked","done"]},
    "assignees":{"type":"array","items":{"type":"string","format":"email"}},
    "labels":{"type":"array","items":{"type":"string"}},
    "due":{"type":"object","properties":{"after":{"type":"string","format":"date"},"before":{"type":"string","format":"date"}}}
  }},
  "sort":{"type":"string","enum":["relevance","updated","due"],"default":"relevance"},
  "limit":{"type":"integer","minimum":1,"maximum":100,"default":20}
}}`,
		Slow: false, Fails: false,
	},
	{
		Name:        "get_task",
		Title:       "Get task",
		Description: "Get one task by its key, for example ACME-101.",
		ReadOnly:    true, Destructive: false, Idempotent: true, OpenWorld: false,
		InputSchema: `{"type":"object","required":["key"],"properties":{"key":{"type":"string","pattern":"^[A-Z]+-[0-9]+$"}}}`,
		Slow:        false, Fails: false,
	},
	{
		Name:        "list_projects",
		Title:       "List projects",
		Description: "List every project in the workspace.",
		ReadOnly:    true, Destructive: false, Idempotent: true, OpenWorld: false,
		InputSchema: noParams,
		Slow:        false, Fails: false,
	},
	{
		Name:        "list_labels",
		Title:       "List labels",
		Description: "List the labels tasks can carry.",
		ReadOnly:    true, Destructive: false, Idempotent: true, OpenWorld: false,
		InputSchema: noParams,
		Slow:        false, Fails: false,
	},
	{
		Name:        "create_task",
		Title:       "Create task",
		Description: "Create a task in a project.",
		ReadOnly:    false, Destructive: false, Idempotent: false, OpenWorld: false,
		InputSchema: `{"type":"object","required":["project","title"],"properties":{
  "project":{"type":"string"},
  "title":{"type":"string","maxLength":200},
  "priority":{"type":"string","enum":["low","medium","high","urgent"]},
  "assignee":{"type":"string","format":"email"},
  "labels":{"type":"array","items":{"type":"string"}},
  "checklist":{"type":"array","items":{"type":"object","required":["text"],"properties":{"text":{"type":"string"},"done":{"type":"boolean"}}}}
}}`,
		Slow: false, Fails: false,
	},
	{
		Name:        "update_task",
		Title:       "Update task",
		Description: "Change a task's title, status, priority or assignee.",
		ReadOnly:    false, Destructive: false, Idempotent: true, OpenWorld: false,
		InputSchema: `{"type":"object","required":["key"],"properties":{
  "key":{"type":"string"},
  "status":{"type":"string","enum":["open","in_progress","blocked","done"]},
  "priority":{"type":"string","enum":["low","medium","high","urgent"]},
  "assignee":{"type":"string","format":"email"}
}}`,
		Slow: false, Fails: false,
	},
	{
		Name:        "delete_task",
		Title:       "Delete task",
		Description: "Delete a task and its comments. This cannot be undone.",
		ReadOnly:    false, Destructive: true, Idempotent: true, OpenWorld: false,
		InputSchema: `{"type":"object","required":["key"],"properties":{"key":{"type":"string"}}}`,
		Slow:        false, Fails: false,
	},
	{
		Name:        "archive_project",
		Title:       "Archive project",
		Description: "Archive a project. Its tasks become read-only.",
		ReadOnly:    false, Destructive: true, Idempotent: true, OpenWorld: false,
		InputSchema: `{"type":"object","required":["project"],"properties":{"project":{"type":"string"},"reason":{"type":"string"}}}`,
		Slow:        false, Fails: false,
	},
	{
		Name:  "generate_quarterly_workspace_activity_report_for_all_teams_and_projects",
		Title: "Generate quarterly workspace activity report",
		Description: "Build the quarterly activity report for every team and project in the workspace.\n\n" +
			"The report counts tasks opened, closed and reopened per team, the median time to close,\n" +
			"overdue work by assignee, and the projects with the most churn.\n\n" +
			"It reads every task in the quarter, so it is slow: expect a few seconds per call.\n" +
			"Results are cached for an hour; pass refresh=true to rebuild them.",
		ReadOnly: true, Destructive: false, Idempotent: true, OpenWorld: false,
		InputSchema: `{"type":"object","properties":{
  "quarter":{"type":"string","enum":["Q1","Q2","Q3","Q4"]},
  "year":{"type":"integer","minimum":2020},
  "teams":{"type":"array","items":{"type":"string"}},
  "refresh":{"type":"boolean","default":false}
}}`,
		Slow: true, Fails: false,
	},
	{
		Name:        "sync_external_calendar",
		Title:       "Sync external calendar",
		Description: "Push task due dates to the team's external calendar.",
		ReadOnly:    false, Destructive: false, Idempotent: true, OpenWorld: true,
		InputSchema: `{"type":"object","properties":{"calendar":{"type":"string"},"since":{"type":"string","format":"date"}}}`,
		Slow:        false, Fails: true,
	},
}
