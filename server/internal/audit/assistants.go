package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionAssistantToolCall Action = "assistant:tool_call"
)

// maxAuditToolCallParamsBytes caps the size of the tool call params payload
// persisted in audit log metadata. Larger payloads are truncated and flagged
// with params_truncated so consumers know the record is partial.
const maxAuditToolCallParamsBytes = 8 * 1024

// auditSecretKeyPattern matches JSON object keys whose values are likely to
// hold credentials and must not be persisted in the audit trail.
var auditSecretKeyPattern = regexp.MustCompile(`(?i)token|secret|password|authorization`)

// LogAssistantToolCallEvent records an assistant-initiated tool call. The
// subject is the assistant; the tool, toolset, thread and (scrubbed) params
// are carried in metadata. The thread id identifies the activation that the
// call belongs to — the originating trigger_instance_id is stamped on
// assistant_thread_events rows and is intentionally not resolved here to keep
// the tool call hot path free of extra queries.
type LogAssistantToolCallEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	AssistantURN urn.Assistant
	Thread       uuid.UUID
	Chat         string
	ToolsetSlug  string
	ToolName     string
	ToolURN      urn.Tool
	Params       json.RawMessage
}

func (l *Logger) LogAssistantToolCall(ctx context.Context, dbtx repo.DBTX, event LogAssistantToolCallEvent) error {
	action := ActionAssistantToolCall

	params, truncated := sanitizeToolCallParams(event.Params)

	meta := map[string]any{
		"toolset_slug": event.ToolsetSlug,
		"tool_name":    event.ToolName,
		"tool_urn":     event.ToolURN.String(),
	}
	if event.Thread != uuid.Nil {
		meta["thread_id"] = event.Thread.String()
	}
	if event.Chat != "" {
		meta["chat_id"] = event.Chat
	}
	if len(params) > 0 {
		meta["params"] = params
	}
	if truncated {
		meta["params_truncated"] = true
	}

	metadata, err := marshalAuditPayload(meta)
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.AssistantURN.ID.String(),
		SubjectType:        string(subjectTypeAssistant),
		SubjectDisplayName: conv.ToPGTextEmpty(event.ToolName),
		SubjectSlug:        conv.ToPGTextEmpty(event.ToolsetSlug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.AssistantToolCallV1})
}

// sanitizeToolCallParams prepares raw tool call arguments for persistence in
// audit metadata: secret-shaped values are redacted, and the result is capped
// at maxAuditToolCallParamsBytes. The returned payload is always valid JSON
// (or nil); when the cap is exceeded the payload is replaced with a JSON
// string holding the leading bytes and the second return value is true.
func sanitizeToolCallParams(raw json.RawMessage) (json.RawMessage, bool) {
	if len(raw) == 0 {
		return nil, false
	}

	var decoded any
	if err := json.Unmarshal(raw, &decoded); err == nil {
		encoded, merr := json.Marshal(redactSecretValues(decoded))
		if merr != nil {
			return nil, false
		}
		raw = encoded
	} else {
		// Not valid JSON: preserve it as a JSON string so the surrounding
		// metadata document stays well-formed.
		encoded, merr := json.Marshal(string(raw))
		if merr != nil {
			return nil, false
		}
		raw = encoded
	}

	if len(raw) <= maxAuditToolCallParamsBytes {
		return raw, false
	}

	// json.Marshal of a string cannot fail; invalid UTF-8 from cutting a rune
	// in half is replaced with U+FFFD.
	truncated, err := json.Marshal(string(raw[:maxAuditToolCallParamsBytes]))
	if err != nil {
		return nil, true
	}

	return truncated, true
}

// redactSecretValues walks a decoded JSON document and replaces the value of
// any object key matching auditSecretKeyPattern with a redaction marker.
func redactSecretValues(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			if auditSecretKeyPattern.MatchString(key) {
				v[key] = "[REDACTED]"
				continue
			}
			v[key] = redactSecretValues(item)
		}
		return v
	case []any:
		for i, item := range v {
			v[i] = redactSecretValues(item)
		}
		return v
	default:
		return value
	}
}
