//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/sessionhandoff"
)

// SessionRecaller is the narrow boundary the session-recall tools call through,
// so unit tests can model the service without a database.
type SessionRecaller interface {
	ListMySessions(ctx context.Context, principal Principal, input ListMySessionsInput) (ListMySessionsOutput, error)
	ContinueSession(ctx context.Context, principal Principal, input ContinueSessionInput) (ContinueSessionOutput, error)
	CreateSessionHandoffLink(ctx context.Context, principal Principal, input CreateSessionHandoffLinkInput) (CreateSessionHandoffLinkOutput, error)
}

const (
	listMySessionsDescription           = "List your own captured coding-agent sessions in this organization — session IDs, titles, summaries, project, working directory, and last activity. Use this first when a user asks to create or share a handoff link unless they already selected an exact session from a displayed list. Present a concise numbered chooser showing each session_id, title, and summary labeled as the description; include project and last activity only when useful. Never ask the user to remember or type an ID, never returns other users' sessions, and never returns transcript content."
	continueSessionDescription          = "Render a redacted handoff digest of one of your own previous sessions so its work can continue here. Sensitive values found by risk scanning appear masked, and tool inputs/outputs are omitted (tool names are retained). Every call records a lineage edge and an entry in the organization's audit log. This is not a transcript or log reader, and it cannot access other users' sessions."
	createSessionHandoffLinkDescription = "Create a copyable burn-after-read web URL containing a risk-masked, redacted handoff digest of one of the user's own captured sessions. Never ask the user to know or type a session ID from memory: first use list_my_sessions and present a numbered chooser with session_id, title, and summary as the description unless the user already selected an exact displayed session. Call with confirmed=true only after the user explicitly confirms that exact displayed session and exact clamped lifetime. Anyone holding this bearer URL can read the digest once, and link previewers may consume it before the intended recipient. It expires quickly: after 900 seconds by default, clamped to 60..3600 seconds."
)

var sessionRecallToolMeta = ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeNone}

func listMySessionsTool() *mcp.Tool {
	return &mcp.Tool{Name: "list_my_sessions", Title: "List My Sessions", Description: listMySessionsDescription, Annotations: readOnlyAnnotations()}
}
func continueSessionTool() *mcp.Tool {
	return &mcp.Tool{Name: "continue_session", Title: "Continue Session", Description: continueSessionDescription, Annotations: continueSessionAnnotations()}
}
func createSessionHandoffLinkTool() *mcp.Tool {
	return &mcp.Tool{Name: "create_session_handoff_link", Title: "Create Session Handoff Link", Description: createSessionHandoffLinkDescription, Annotations: createSessionHandoffLinkAnnotations()}
}

func registerSessionRecallTools(reg *Registrar, svc SessionRecaller) {
	addTool(reg, listMySessionsTool(), sessionRecallToolMeta, func(ctx context.Context, _ *mcp.CallToolRequest, input ListMySessionsInput) (*mcp.CallToolResult, ListMySessionsOutput, error) {
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
	addTool(reg, createSessionHandoffLinkTool(), sessionRecallToolMeta, func(ctx context.Context, _ *mcp.CallToolRequest, input CreateSessionHandoffLinkInput) (*mcp.CallToolResult, CreateSessionHandoffLinkOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, CreateSessionHandoffLinkOutput{}, err
		}
		if svc == nil {
			return nil, CreateSessionHandoffLinkOutput{}, errSessionHandoffUnavailable
		}
		output, err := svc.CreateSessionHandoffLink(ctx, principal, input)
		if err != nil {
			if result, ok := sessionRecallToolResult(err); ok {
				return result, CreateSessionHandoffLinkOutput{}, nil
			}
			return nil, CreateSessionHandoffLinkOutput{}, fmt.Errorf("create session handoff link: %w", err)
		}
		return nil, output, nil
	})
	addTool(reg, continueSessionTool(), sessionRecallToolMeta, func(ctx context.Context, _ *mcp.CallToolRequest, input ContinueSessionInput) (*mcp.CallToolResult, ContinueSessionOutput, error) {
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
	case errors.Is(err, errSessionHandoffUnavailable):
		payload = featureUnavailableResult{Code: unavailableCode, Feature: "session_handoff_link", Message: "Session handoff links are unavailable in the current rollout."}
	case errors.Is(err, ErrUnavailable):
		payload = featureUnavailableResult{Code: unavailableCode, Feature: "session_recall", Message: "Session recall is unavailable in the current rollout."}
	case errors.Is(err, errSessionRecallDisabled):
		payload = featureUnavailableResult{
			Code:    unavailableCode,
			Feature: "session_portability",
			Message: "Session recall is not enabled for this organization.",
		}
	case errors.Is(err, errSessionHandoffConfirmationRequired):
		confirmation := &sessionHandoffConfirmationError{ttlSeconds: int(sessionhandoff.DefaultHandoffTTL / time.Second)}
		_ = errors.As(err, &confirmation)
		payload = operationBudgetResult{Code: "confirmation_required", Message: fmt.Sprintf("Before retrying, display the selected session title and session_id from list_my_sessions and the exact clamped lifetime of %d seconds, then ask the user to explicitly confirm that exact session and lifetime. Do not read or export session content before confirmation.", confirmation.ttlSeconds)}
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

// continue_session writes a lineage edge and an audit event on every call.
func continueSessionAnnotations() *mcp.ToolAnnotations {
	return writeAnnotations(false, false, false)
}

func createSessionHandoffLinkAnnotations() *mcp.ToolAnnotations {
	return writeAnnotations(false, false, false)
}

// Each stub declares the audiences its live counterpart declares, so the tools
// do not appear on and disappear from a surface as the rollout flips.
func unavailableSessionTool[I, O any](feature, message string) mcp.ToolHandlerFor[I, O] {
	return func(_ context.Context, _ *mcp.CallToolRequest, _ I) (*mcp.CallToolResult, O, error) {
		result := featureUnavailableResult{Code: unavailableCode, Feature: feature, Message: message}
		content, err := json.Marshal(result)
		if err != nil {
			return nil, *new(O), fmt.Errorf("encode unavailable session tool: %w", err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, *new(O), nil
	}
}

// Typed unavailable handlers retain each live tool's exact inferred schemas.
func registerUnavailableSessionRecallTools(reg *Registrar) {
	addTool(reg, listMySessionsTool(), sessionRecallToolMeta,
		unavailableSessionTool[ListMySessionsInput, ListMySessionsOutput]("session_recall", "Session recall is unavailable in the current rollout."))
	addTool(reg, createSessionHandoffLinkTool(), sessionRecallToolMeta,
		unavailableSessionTool[CreateSessionHandoffLinkInput, CreateSessionHandoffLinkOutput]("session_handoff_link", "Session handoff links are unavailable in the current rollout."))
	addTool(reg, continueSessionTool(), sessionRecallToolMeta,
		unavailableSessionTool[ContinueSessionInput, ContinueSessionOutput]("session_recall", "Session recall is unavailable in the current rollout."))
}
