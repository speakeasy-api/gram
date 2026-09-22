//nolint:exhaustruct // MCP schemas rely on documented optional zero values.
package platformmcp

import (
	"context"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerRiskFindingsTool(reg *Registrar, service riskFindingsLister) {
	tool := &mcp.Tool{
		Name: "list_watchdog_findings", Title: "List Watchdog Findings",
		Description: "Read rule-level Watchdog alerts, default critical in the last 24 hours by detection time. Returns groups of findings per rule, affected users, affected clients (observed chat sources), and one fully redacted stored display sample per alert. All live findings contribute, including disabled policy matches. Severity uses maximum current nondeleted contributing policy scores with dashboard category fallback. Up to 100 alerts, explicit truncation, and exact full-window total_count and total_alerts after severity filtering. Over 1000 rules fails closed; narrow the time window. Optional group_by adds independent per-alert finding histograms, not dashboard sections. Requires Watchdog and ClickHouse risk listing enabled.",
		Annotations: readOnlyAnnotations(),
		InputSchema: projectSelectorSchema(map[string]*jsonschema.Schema{
			"from":     {Type: "string", Description: "Inclusive detection time (created_at) in RFC3339. Defaults to 24h before to; at most 90 days old."},
			"to":       {Type: "string", Description: "Exclusive detection time (created_at) in RFC3339. Defaults to now. No future bounds; maximum interval 31 days."},
			"severity": {Type: "string", Enum: []any{"critical", "high", "medium", "low", "all"}, Description: "Exact severity band; default critical. Use all for no severity filter. Critical is rule signal score >=9."},
			"group_by": {Type: "array", Items: &jsonschema.Schema{Type: "string", Enum: []any{"severity", "data_type", "team", "app", "user"}}, MaxItems: new(5), UniqueItems: true, Description: "Optional independent per-alert finding histograms. Each returns top 200 buckets plus explicit truncation. Empty bucket means unknown. Users are organization-scoped pseudonyms; app is observed chat_source."},
		}, nil),
	}
	meta := ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: bothAudiences, ProjectScope: ProjectScopeDefaultable}
	if service == nil || !service.valid() {
		tool.Description += " Findings are unavailable in this deployment."
		addTool(reg, tool, meta, unavailableRiskReadTool(reg, tool.Name))
		return
	}
	addTool(reg, tool, meta, func(ctx context.Context, _ *mcp.CallToolRequest, input ListRiskFindingsInput) (*mcp.CallToolResult, ListRiskFindingsOutput, error) {
		return riskReadToolCall(ctx, reg.riskTelemetry, tool.Name, func(principal Principal) (ListRiskFindingsOutput, error) { return service.List(ctx, principal, input) })
	})
}
