package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
)

var (
	// claudeSessionNamespace is the UUIDv5 namespace for Claude Code session IDs.
	// This ensures deterministic UUID generation from session ID strings.
	claudeSessionNamespace = uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")

	// ErrChatNotFound indicates the chat (conversation) does not exist.
	ErrChatNotFound = errors.New("chat not found")
)

// isForeignKeyViolation checks if the error is a PostgreSQL foreign key constraint violation.
// This indicates that the referenced chat does not exist.
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		// 23503 is PostgreSQL's foreign_key_violation error code
		return pgErr.Code == "23503"
	}
	return false
}

// sessionIDToUUID converts a Claude Code session_id string to a UUID.
// The session_id is expected to already be a valid UUID string.
// If parsing fails, falls back to generating a deterministic UUIDv5 from the session_id.
func sessionIDToUUID(sessionID string) uuid.UUID {
	// Try to parse the session ID as a UUID directly
	parsedUUID, err := uuid.Parse(sessionID)
	if err == nil {
		return parsedUUID
	}

	// Fallback: generate a deterministic UUIDv5 from the session ID string
	return uuid.NewSHA1(claudeSessionNamespace, []byte(sessionID))
}

// makeHookResult creates a ClaudeHookResult, attaching HookSpecificOutput only
// for hook events whose Claude Code response schema permits it. Stop, SessionStart,
// SessionEnd, Notification, and PostToolUseFailure must NOT carry hookSpecificOutput
// — Claude Code rejects unknown variants with "Hook JSON output validation failed".
func makeHookResult(hookEventName string) *gen.ClaudeHookResult {
	result := &gen.ClaudeHookResult{
		HookSpecificOutput: nil,
		Continue:           nil,
		StopReason:         nil,
		SuppressOutput:     nil,
		SystemMessage:      nil,
		Decision:           nil,
		Reason:             nil,
	}
	if hookEventName == "PreToolUse" {
		result.HookSpecificOutput = &HookSpecificOutput{
			HookEventName:            &hookEventName,
			AdditionalContext:        nil,
			PermissionDecision:       nil,
			PermissionDecisionReason: nil,
		}
	}
	return result
}

// constructBlockResponse builds a hook result that blocks the current event
// using the JSON shape Claude Code expects for the given hook. Per
// https://code.claude.com/docs/en/hooks#decision-control:
//
//   - UserPromptSubmit / PostToolUse / Stop / SubagentStop: top-level
//     `decision: "block"` + free-text `reason`. The reason is surfaced to
//     the user (UserPromptSubmit) or to Claude (PostToolUse / Stop).
//   - PreToolUse: nested `hookSpecificOutput.permissionDecision: "deny"`
//   - `permissionDecisionReason`. Top-level `decision` is rejected.
//
// Other events (SessionStart, SessionEnd, Notification, PostToolUseFailure)
// cannot block at all and must not be passed in.
func constructBlockResponse(hookEventName, reason string) *gen.ClaudeHookResult {
	result := makeHookResult(hookEventName)
	if hookEventName == "PreToolUse" {
		deny := "deny"
		if output, ok := result.HookSpecificOutput.(*HookSpecificOutput); ok {
			output.PermissionDecision = &deny
			output.PermissionDecisionReason = &reason
		}
		// systemMessage renders as a warning in the user's terminal;
		// permissionDecisionReason is what Claude itself sees and may quote
		// back. Set both so the user gets visible feedback regardless of how
		// the client renders the deny.
		result.SystemMessage = &reason
		return result
	}
	block := "block"
	result.Decision = &block
	result.Reason = &reason
	return result
}

// handleUserPromptSubmit captures the user's prompt text as a chat message.
// When a blocking risk policy matches, it returns 200 with a top-level
// `decision: "block"` + `reason`, the shape Claude Code documents for
// UserPromptSubmit. Claude Code erases the prompt from context and surfaces
// the reason to the user. Returning 200 with a shaped body (instead of 4xx
// or exit-code-2) is what makes the block reason render — stderr-only
// blocks don't carry the reason field at all.
// https://code.claude.com/docs/en/hooks#decision-control
func (s *Service) handleUserPromptSubmit(ctx context.Context, ev *hookevents.UserPromptSubmit) (*gen.ClaudeHookResult, error) {
	payload := claudePayloadFromEvent(ev.Event)
	if payload == nil {
		return makeHookResult(ev.RawEventType), nil
	}
	if s.riskScanner != nil && ev.Prompt != "" && ev.ConversationID != "" {
		if scanResult := s.scanUserPromptForEnforcement(ctx, ev); scanResult != nil {
			auditReason := fmt.Sprintf("Speakeasy blocked this prompt: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			userReason := renderUserBlockReason(scanResult.UserMessage, auditReason)
			// ClickHouse always gets the technical reason; the user_message
			// override only changes what the agent / end user sees.
			if metadata, err := s.getSessionMetadata(ctx, *payload.SessionID); err == nil {
				s.writeClaudeBlockToClickHouse(ctx, payload, &metadata, auditReason)
			}
			return constructBlockResponse(payload.HookEventName, userReason), nil
		}
	}
	return makeHookResult(ev.RawEventType), nil
}

// handleStop captures the assistant's final response text.
// Note: If the Stop event includes tool calls, those are handled separately by PreToolUse events,
// so we skip creating duplicate messages here.
func (s *Service) handleStop(ctx context.Context, ev *hookevents.Stop) (*gen.ClaudeHookResult, error) {
	return makeHookResult(ev.RawEventType), nil
}

// handleSessionEnd finalizes the session by updating the timestamp.
func (s *Service) handleSessionEnd(ctx context.Context, ev *hookevents.SessionEnd) (*gen.ClaudeHookResult, error) {
	return makeHookResult(ev.RawEventType), nil
}

// handleNotification handles notification events (permission_prompt, idle_prompt, etc.)
func (s *Service) handleNotification(ctx context.Context, ev *hookevents.Notification) (*gen.ClaudeHookResult, error) {
	return makeHookResult(ev.RawEventType), nil
}

func claudeLastUserPromptIDFromAdditionalData(additionalData map[string]any) string {
	if additionalData == nil {
		return ""
	}
	if v, ok := additionalData["LastUserPromptID"].(string); ok {
		return v
	}
	return ""
}

// marshalToJSON converts any value to a JSON string.
func marshalToJSON(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
