package platformmcp

import (
	"context"
	"errors"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

// Team memory is external-only: it needs a person's explicit memory grant,
// which a managed assistant turn does not carry.
var (
	discoveryMemoryRead  = []authz.Scope{authz.ScopeMemoryRead}
	discoveryMemoryWrite = []authz.Scope{authz.ScopeMemoryWrite}
)

func registerTeamMemoryTools(reg *Registrar, service *TeamMemoryService) {
	rememberMeta := ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryMemoryWrite}
	rememberSchema := teamMemoryProjectSchema(map[string]*jsonschema.Schema{
		"content": stringSchema("The fact to remember, in one or two sentences: a decision and its reason, a convention, or a gotcha. Detected secrets are masked before it is stored. Up to 4000 characters.", 1, memoMaxRunes),
		"chat_id": uuidSchema("Optional exact ID of the chat the fact came from, in the same project."),
	}, []string{"content"})
	remember := &mcp.Tool{Meta: nil, OutputSchema: nil, Icons: nil, Name: rememberToolName, Title: "Remember for the Team", Description: "Save a fact to one project's team memory so later sessions can find it with team memory search. Use it for decisions, conventions and gotchas worth keeping, not for raw output. The memo is attributed to you and shared with everyone who can read the project's team memory. Requires an explicit memory:write grant; the write is audited.", Annotations: writeAnnotations(false, false, false), InputSchema: rememberSchema}
	if !service.valid() {
		addTool(reg, remember, rememberMeta, unavailableTeamMemoryHandler[RememberInput, RememberOutput]())
		return
	}
	addTool(reg, remember, rememberMeta, teamMemoryHandler(service.Remember))
}

// teamMemoryProjectSchema requires exactly one project selector; oneOf
// rejects a call naming both before it reaches the service.
func teamMemoryProjectSchema(properties map[string]*jsonschema.Schema, required []string) *jsonschema.Schema {
	schema := projectSelectorSchema(properties, required)
	schema.Properties["project_id"].Description = "Exact project ID. Provide exactly one project selector."
	schema.Properties["project_slug"].Description = "Exact project slug. Provide exactly one project selector."
	schema.OneOf = []*jsonschema.Schema{{Required: []string{"project_id"}}, {Required: []string{"project_slug"}}}
	return schema
}

func teamMemoryHandler[In, Out any](call func(context.Context, Principal, In) (Out, error)) mcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input In) (*mcp.CallToolResult, Out, error) {
		return principalToolCall(ctx, teamMemoryToolResult, func(principal Principal) (Out, error) { return call(ctx, principal, input) })
	}
}

func teamMemoryToolResult(err error) (*mcp.CallToolResult, bool) {
	if result, ok := externalAuthorizationToolResult(err); ok {
		return result, true
	}
	if errors.Is(err, errTeamMemoryInvalid) || errors.Is(err, ErrRiskReadInvalid) {
		return chatSearchRefusal(operationBudgetResult{Code: "invalid_request", Reason: "", SetupCategory: "", Actions: nil, Message: "Choose exactly one project and valid IDs, and keep memo content between 1 and 4000 characters."}), true
	}
	if errors.Is(err, errTeamMemoryNotEnabled) {
		return chatSearchRefusal(operationBudgetResult{Code: "feature_not_enabled", Reason: "", SetupCategory: "", Actions: nil, Message: "Team memory is not switched on for this organization yet."}), true
	}
	if errors.Is(err, ErrRiskReadNotFound) {
		return chatSearchRefusal(operationBudgetResult{Code: "not_found", Reason: "", SetupCategory: "", Actions: nil, Message: "That project or chat is not available here. Use an exact project and a chat from that project."}), true
	}
	return operationBudgetToolResult(err)
}

func unavailableTeamMemoryHandler[In, Out any]() mcp.ToolHandlerFor[In, Out] {
	return func(_ context.Context, _ *mcp.CallToolRequest, _ In) (*mcp.CallToolResult, Out, error) {
		var zero Out
		return nil, zero, &ToolRefusalError{Code: unavailableCode, Payload: `{"code":"feature_unavailable","feature":"team_memory","message":"Team memory is not available on this server."}`}
	}
}
