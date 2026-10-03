//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const listChatsToolName = "list_chats"

// listChatsMeta declares list_chats' audience and authority. Admitted to
// the assistant because the read is connection-less and carries no content;
// org:admin because a project-wide listing crosses every member's chats.
var listChatsMeta = ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: bothAudiences, ProjectScope: ProjectScopeDefaultable}

func listChatsTool(description string) *mcp.Tool {
	return &mcp.Tool{
		Name:        listChatsToolName,
		Title:       "List Chats",
		Description: description,
		Annotations: readOnlyAnnotations(),
		InputSchema: projectSelectorSchema(map[string]*jsonschema.Schema{
			"window":         {Type: "string", Enum: []any{string(DiagnosticWindowLastHour), string(DiagnosticWindowLastDay), string(DiagnosticWindowLastWeek), string(DiagnosticWindowLastMonth)}, Description: "Activity window: 1h, 24h, 7d (default), or 30d. A chat is listed when its last message is at or after the window start and it was created at or before the window end."},
			"risk":           {Type: "string", Enum: []any{ChatRiskWithFindings, ChatRiskWithoutFindings}, Description: "Optional risk presence filter: with_findings keeps chats with at least one live finding, without_findings keeps the rest. Omit for no filter."},
			"source":         stringSchema("Optional exact chat source label to keep, as reported in a previous row's source field.", 1, maxChatSourceLength),
			"assistant_id":   uuidSchema("Optional assistant ID to keep only that assistant's threads."),
			"user_reference": stringSchema("Optional person reference from a previous list_chats row in this project, to keep only that person's chats.", 1, 1024),
			"limit":          {Type: "integer", Minimum: new(float64(1)), Maximum: new(float64(maxChatListLimit)), Description: "Maximum chats to return; defaults to 20 and is capped at 50."},
			"cursor":         stringSchema("Opaque cursor returned by a previous list_chats result.", 1, 2048),
		}, nil),
	}
}

func registerChatMetadataTools(reg *Registrar, service *ChatMetadataService) {
	addTool(reg, listChatsTool("List one project's chats active in a window of up to 30 days, newest activity first, as metadata only. Each chat is reduced to when it started and was last active, how many messages it holds, how many live risk findings it carries, the app and account type that produced it, the assistant behind it when there was one, a masked participant, and a short-lived person reference that narrows a follow-up listing. Narrow by window, risk presence, source, assistant, or person reference. Constraints: this is not a transcript reader or a search; no titles, messages, prompts, tool inputs or outputs, or raw identities are returned, pages are bounded and a limit above 50 is capped rather than refused, every count is in chats (one row per chat, total_matches in chats, at most 500 chats walked per listing), a cursor pins the window it was minted for, and cursors and references expire and are bound to this session."), listChatsMeta, func(ctx context.Context, _ *mcp.CallToolRequest, input ListChatsInput) (*mcp.CallToolResult, ListChatsOutput, error) {
		return principalToolCall(ctx, chatMetadataToolResult, func(principal Principal) (ListChatsOutput, error) {
			return service.List(ctx, principal, input)
		})
	})
}

// chatMetadataToolResult maps the listing's refusals to structured payloads,
// deferring budgets to the shared mapper. A project, reference, or cursor that
// cannot be resolved is one not_found: distinguishing "expired" from "unknown"
// would confirm that a reference once existed.
func chatMetadataToolResult(err error) (*mcp.CallToolResult, bool) {
	var result operationBudgetResult
	switch {
	case errors.Is(err, ErrChatListInvalid), errors.Is(err, ErrRiskReadInvalid), errors.Is(err, ErrDiagnosticWindowInvalid), errors.Is(err, ErrDiagnosticWindowTooLong):
		result = operationBudgetResult{Code: "invalid_request", Message: "That chat listing request is not valid. Use one project selector, a window of 1h, 24h, 7d, or 30d, a risk filter of with_findings or without_findings, and a source label or assistant ID from a previous result."}
	case errors.Is(err, ErrRiskReadNotFound):
		result = operationBudgetResult{Code: "not_found", Message: "That project is not one this organization holds. Choose one returned by list_projects."}
	case errors.Is(err, ErrSubjectReferenceNotFound):
		result = operationBudgetResult{Code: "not_found", Message: "That cursor or person reference is not available here. List the chats again with the same filters and use a value from the new result."}
	default:
		return operationBudgetToolResult(err)
	}
	content, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, true
}
