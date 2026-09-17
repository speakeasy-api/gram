//nolint:exhaustruct // MCP schemas rely on documented optional zero values.
package platformmcp

import (
	"context"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerRiskFindingsTool(reg *Registrar, service *RiskFindingsService) {
	tool := &mcp.Tool{
		Name: "list_risk_findings", Title: "List Watchdog Findings",
		Description: "Read privacy-safe Watchdog findings for a digest: defaults to critical findings in the last 24 hours. Returns up to 50 findings, a signed next_cursor, full-window total_count and independent group counts by severity, data_type (stored category), team, app (chat source), or pseudonymous user. Current enabled policy scores define severity, not confidence. Missing attribution remains unknown. Never returns matched content. Requires Watchdog and ClickHouse risk listing enabled. Repeat returned from/to and filters on subsequent pages.",
		Annotations: readOnlyAnnotations(),
		InputSchema: projectSelectorSchema(map[string]*jsonschema.Schema{
			"from":     {Type: "string", Description: "Inclusive message event time in RFC3339. Defaults to 24h before to; at most 90 days old. Required with cursor."},
			"to":       {Type: "string", Description: "Exclusive message event time in RFC3339. Defaults to now. No future bounds; maximum interval 31 days. Required with cursor."},
			"severity": {Type: "string", Enum: []any{"critical", "high", "medium", "low", "all"}, Description: "Exact severity band; default critical. Use all for no severity filter. Critical is current policy score >=9."},
			"group_by": {Type: "array", Items: &jsonschema.Schema{Type: "string", Enum: []any{"severity", "data_type", "team", "app", "user"}}, MaxItems: new(5), UniqueItems: true, Description: "Independent groupings, default severity. Each returns top 200 buckets plus an explicit truncation flag. Empty bucket means unknown."},
			"cursor":   {Type: "string", MaxLength: new(4096), Description: "Opaque next_cursor from the previous response. Bound to principal, organization, project, time interval and filters."},
		}, nil),
	}
	meta := ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: bothAudiences, ProjectScope: ProjectScopeDefaultable}
	if !service.valid() {
		tool.Description += " Findings are unavailable in this deployment."
		addTool(reg, tool, meta, unavailableRiskReadTool(reg, tool.Name))
		return
	}
	addTool(reg, tool, meta, func(ctx context.Context, _ *mcp.CallToolRequest, input ListRiskFindingsInput) (*mcp.CallToolResult, ListRiskFindingsOutput, error) {
		return riskReadToolCall(ctx, reg.riskTelemetry, tool.Name, func(principal Principal) (ListRiskFindingsOutput, error) { return service.List(ctx, principal, input) })
	})
}
