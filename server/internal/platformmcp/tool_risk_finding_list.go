//nolint:exhaustruct // MCP schemas rely on documented optional zero values.
package platformmcp

import (
	"context"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	riskFindingListToolName   = "list_risk_findings"
	riskFindingByChatToolName = "list_risk_findings_by_chat"
	riskRuleBreakdownToolName = "get_risk_rule_breakdown"
)

// registerRiskFindingListTools serves the three per-finding reads live when the
// service is usable, and as "not switched on" stubs otherwise, so the tools
// always exist in the manifest.
func registerRiskFindingListTools(reg *Registrar, service riskFindingListLister) {
	available := service != nil && service.valid()
	registerRiskFindingListTool(reg, &mcp.Tool{
		Name: riskFindingListToolName, Title: "List Risk Findings",
		Description: "List individual risk findings in an exact project or the organization's literal default project, newest message first, with the matched value redacted to a length and hash fingerprint that never reaches the model. Filter by message-time window, policy, concrete MCP server, chat, category, rule substring, user substring, assistant linkage, or unique match; fetch one finding by result_id, or every finding on one MCP tool call by execution_id; page with an opaque cursor, 25 per page by default and at most 50. MCP findings can be chatless and return execution_id (the tool call that raised them), mcp_server_id, meta_mcp_server_id, toolset_id, tool_name, phase, mediation_surface, mcp_method, principal_kind, identity_stamped, and enforcement_outcome. Each finding carries a stable id for later dismissal, its severity band and score, and an organization-scoped user pseudonym shared with list_watchdog_findings. Prefer get_risk_rule_breakdown to size a finding set, and list_watchdog_findings for severity-first rule triage.",
		Annotations: readOnlyAnnotations(), InputSchema: riskFindingListSchema(),
	}, available, func(ctx context.Context, principal Principal, input ListRiskFindingPageInput) (ListRiskFindingPageOutput, error) {
		return service.List(ctx, principal, input)
	})
	registerRiskFindingListTool(reg, &mcp.Tool{
		Name: riskFindingByChatToolName, Title: "List Risk Findings By Chat",
		Description: "List chat sessions with live risk findings in an exact project or the organization's literal default project, with each chat's finding count, latest detection time and an organization-scoped user pseudonym. Pages by chat id with an opaque cursor, 25 per page by default and at most 50. Use list_risk_findings with chat_id to read one chat's findings.",
		Annotations: readOnlyAnnotations(), InputSchema: riskFindingByChatSchema(),
	}, available, func(ctx context.Context, principal Principal, input ListRiskFindingsByChatInput) (ListRiskFindingsByChatOutput, error) {
		return service.ListByChat(ctx, principal, input)
	})
	registerRiskFindingListTool(reg, &mcp.Tool{
		Name: riskRuleBreakdownToolName, Title: "Get Risk Rule Breakdown",
		Description: "Count live risk findings per rule and detection source for one category over a detection-time window, defaulting to the last seven days and capped at 31 days, in an exact project or the organization's literal default project. Answers volume questions in one small call instead of paginating list_risk_findings. Unlike list_watchdog_findings it filters by category rather than severity band, needs no Watchdog rollout, and returns up to 1000 rules with explicit truncation.",
		Annotations: readOnlyAnnotations(), InputSchema: riskRuleBreakdownSchema(),
	}, available, func(ctx context.Context, principal Principal, input GetRiskRuleBreakdownInput) (GetRiskRuleBreakdownOutput, error) {
		return service.RuleBreakdown(ctx, principal, input)
	})
}

func registerRiskFindingListTool[In, Out any](reg *Registrar, tool *mcp.Tool, available bool, call func(context.Context, Principal, In) (Out, error)) {
	meta := ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: bothAudiences, ProjectScope: ProjectScopeDefaultable}
	if !available {
		tool.Description += " Finding reads are unavailable in this deployment."
		addTool(reg, tool, meta, unavailableRiskReadTool(reg, tool.Name))
		return
	}
	addTool(reg, tool, meta, func(ctx context.Context, _ *mcp.CallToolRequest, input In) (*mcp.CallToolResult, Out, error) {
		return riskReadToolCall(ctx, reg.riskTelemetry, tool.Name, func(principal Principal) (Out, error) { return call(ctx, principal, input) })
	})
}

func riskFindingPageProperties() map[string]*jsonschema.Schema {
	return map[string]*jsonschema.Schema{
		"cursor": {Type: "string", Description: "Opaque cursor returned by the previous page. Replay it with the same filters."},
		"limit":  {Type: "integer", Minimum: new(float64(1)), Maximum: new(float64(riskFindingListMaxLimit)), Description: "Page size; defaults to 25 and cannot exceed 50. Keep it small: a page stays in context for the rest of the turn."},
	}
}

func riskFindingListSchema() *jsonschema.Schema {
	properties := riskFindingPageProperties()
	properties["from"] = &jsonschema.Schema{Type: "string", Description: "Inclusive message time in RFC3339. Omit for no lower bound."}
	properties["to"] = &jsonschema.Schema{Type: "string", Description: "Exclusive message time in RFC3339. Omit for no upper bound."}
	properties["policy_id"] = uuidSchema("Optional exact policy ID, including a disabled policy. Without it, findings from every non-deleted policy are listed.")
	properties["chat_id"] = uuidSchema("Optional exact chat ID. Combines with the other filters.")
	properties["mcp_server_id"] = uuidSchema("Optional exact MCP server ID; only findings on tool calls through that server, including chatless ones.")
	properties["category"] = enumSchema(riskCategoryKeys()...)
	properties["category"].Description = "Optional exact risk category key."
	properties["rule_id"] = stringSchema("Optional case-insensitive substring of the rule identifier, such as secret.", 1, 128)
	properties["user_id"] = stringSchema("Optional case-insensitive substring matched against the chat's external user id.", 1, 256)
	properties["assistant_id"] = uuidSchema("Optional assistant ID; only findings from chats linked to it. Mutually exclusive with non_assistant.")
	properties["non_assistant"] = &jsonschema.Schema{Type: "boolean", Description: "Only findings from chats not linked to any assistant."}
	properties["unique_match"] = &jsonschema.Schema{Type: "boolean", Description: "Collapse to one finding per (policy, rule, matched value), keeping the most recent occurrence."}
	properties["result_id"] = uuidSchema("Optional finding ID. Fetches that one finding, such as one from a shared link, even when it is not on the current page. Returns nothing if it was marked a false positive.")
	properties["execution_id"] = stringSchema("Optional MCP tool call ID, the execution_id a finding returns. Lists every finding raised on that one call, across both its request and response phases, so you can see everything the call triggered. Findings marked as false positives stay hidden.", 1, 128)
	return projectSelectorSchema(properties, nil)
}

func riskFindingByChatSchema() *jsonschema.Schema {
	return projectSelectorSchema(riskFindingPageProperties(), nil)
}

func riskRuleBreakdownSchema() *jsonschema.Schema {
	category := enumSchema(riskCategoryKeys()...)
	category.Description = "Risk category key to break down by rule."
	return projectSelectorSchema(map[string]*jsonschema.Schema{
		"category": category,
		"from":     {Type: "string", Description: "Inclusive detection time in RFC3339. Defaults to the start (UTC) of the day six days before to."},
		"to":       {Type: "string", Description: "Exclusive detection time in RFC3339. Defaults to now. Maximum interval 31 days."},
	}, []string{"category"})
}
