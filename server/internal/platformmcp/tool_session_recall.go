//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SessionRecaller is the narrow boundary the session-recall tools call through,
// so unit tests can model the service without a database.
type SessionRecaller interface {
	ListMySessions(ctx context.Context, principal Principal, input ListMySessionsInput) (ListMySessionsOutput, error)
	ContinueSession(ctx context.Context, principal Principal, input ContinueSessionInput) (ContinueSessionOutput, error)
}

func registerSessionRecallTools(reg *Registrar, svc SessionRecaller) {
	addTool(reg, &mcp.Tool{
		Name:        "list_my_sessions",
		Title:       "List My Sessions",
		Description: "List your own captured coding-agent sessions in this organization — titles, summaries, project, working directory, and last activity. Never returns other users' sessions and never returns transcript content; use continue_session to recall one.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeNone}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListMySessionsInput) (*mcp.CallToolResult, ListMySessionsOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, ListMySessionsOutput{}, err
		}
		if svc == nil {
			return nil, ListMySessionsOutput{}, ErrUnavailable
		}
		output, err := svc.ListMySessions(ctx, principal, input)
		if err != nil {
			if result, ok := sessionRecallToolResult(err); ok {
				return result, ListMySessionsOutput{}, nil
			}
			return nil, ListMySessionsOutput{}, fmt.Errorf("list my sessions: %w", err)
		}
		return nil, output, nil
	})

	// Deliberately NOT read-only: every successful call writes a lineage edge
	// and an audit event, and the description says so.
	addTool(reg, &mcp.Tool{
		Name:        "continue_session",
		Title:       "Continue Session",
		Description: "Render a redacted handoff digest of one of your own previous sessions so its work can continue here. Sensitive values found by risk scanning appear masked, and tool inputs/outputs are omitted (tool names are retained). Every call records a lineage edge and an entry in the organization's audit log. This is not a transcript or log reader, and it cannot access other users' sessions.",
	}, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeNone}, func(ctx context.Context, _ *mcp.CallToolRequest, input ContinueSessionInput) (*mcp.CallToolResult, ContinueSessionOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, ContinueSessionOutput{}, err
		}
		if svc == nil {
			return nil, ContinueSessionOutput{}, ErrUnavailable
		}
		output, err := svc.ContinueSession(ctx, principal, input)
		if err != nil {
			if result, ok := sessionRecallToolResult(err); ok {
				return result, ContinueSessionOutput{}, nil
			}
			return nil, ContinueSessionOutput{}, fmt.Errorf("continue session: %w", err)
		}
		return nil, output, nil
	})
}

// sessionRecallToolResult maps the recall sentinels to structured refusal
// payloads, deferring everything else to the shared budget mapper.
func sessionRecallToolResult(err error) (*mcp.CallToolResult, bool) {
	var payload any
	switch {
	case errors.Is(err, errSessionRecallDisabled):
		payload = featureUnavailableResult{
			Code:    unavailableCode,
			Feature: "session_portability",
			Message: "Session recall is not enabled for this organization.",
		}
	case errors.Is(err, errSessionNotFound):
		payload = operationBudgetResult{
			Code:    "not_found",
			Message: "This session was not found among your own captured sessions. Use list_my_sessions to see the sessions you can continue.",
		}
	default:
		return operationBudgetToolResult(err)
	}
	content, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, true
}

// Each stub declares the audiences its live counterpart declares, so the tools
// do not appear on and disappear from a surface as the rollout flips.
func registerUnavailableSessionRecallTools(reg *Registrar) {
	addTool(reg, &mcp.Tool{
		Name:        "list_my_sessions",
		Title:       "List My Sessions",
		Description: "List your own captured coding-agent sessions. Session recall is not available in the current rollout.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeNone}, unavailableTool("session_recall"))
	addTool(reg, &mcp.Tool{
		Name:        "continue_session",
		Title:       "Continue Session",
		Description: "Recall one of your own previous sessions as a redacted handoff digest. Session recall is not available in the current rollout.",
	}, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeNone}, unavailableTool("session_recall"))
}
