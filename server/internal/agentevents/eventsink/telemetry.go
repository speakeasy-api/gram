package eventsink

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/agentevents"
	"github.com/speakeasy-api/gram/server/internal/agentevents/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

// BuildTelemetryLogs converts an agent event into telemetry log entries. source
// is the provider display name (for example "Cursor") used in human-facing log
// bodies; the raw provider is read from the event for machine-facing fields.
func BuildTelemetryLogs[T any](e agentevents.Event[T], source string) ([]telemetry.LogParams, error) {
	eventType, ok, err := e.EventType()
	if err != nil || !ok {
		return nil, err
	}

	attrs, ok, err := telemetryAttrs(e, eventType, source)
	if err != nil || !ok {
		return nil, err
	}

	params, err := finalizeLogParams(e, attrs)
	if err != nil {
		return nil, err
	}
	return []telemetry.LogParams{params}, nil
}

func telemetryAttrs[T any](e agentevents.Event[T], eventType types.EventType, source string) (map[attr.Key]any, bool, error) {
	switch eventType {
	case types.ToolCallStarted, types.ToolCallCompleted, types.ToolCallFailed,
		types.MCPToolCallStarted, types.MCPToolCallCompleted:
		attrs, err := hookBaseAttrs(e, eventType)
		if err != nil {
			return nil, false, err
		}
		if err := addToolAttrs(attrs, e); err != nil {
			return nil, false, err
		}
		return attrs, true, nil
	case types.SessionEnded:
		attrs, err := hookBaseAttrs(e, eventType)
		if err != nil {
			return nil, false, err
		}
		addUsageAttrs(attrs, e)
		return attrs, true, nil
	case types.SessionStarted:
		attrs, err := hookBaseAttrs(e, eventType)
		if err != nil {
			return nil, false, err
		}
		return attrs, true, nil
	case types.AssistantResponseComplete:
		return usageTelemetryAttrs(e, source)
	default:
		return nil, false, nil
	}
}

func commonAttrs[T any](e agentevents.Event[T], hookSource string) map[attr.Key]any {
	return map[attr.Key]any{
		attr.EventSourceKey:    string(telemetry.EventSourceHook),
		attr.ProjectIDKey:      e.Context.ProjectID,
		attr.OrganizationIDKey: e.Context.OrgID,
		attr.HookSourceKey:     firstNonEmpty(hookSource, string(e.Provider())),
		attr.SpanIDKey:         generateSpanID(),
		attr.TraceIDKey:        generateTraceID(),
	}
}

func addContextAttrs[T any](attrs map[attr.Key]any, e agentevents.Event[T]) {
	setStringValueAttr(attrs, e.Context.UserEmail, attr.UserEmailKey)
	setStringValueAttr(attrs, e.Context.UserID, attr.UserIDKey)
	setStringValueAttr(attrs, e.Context.ConversationID, attr.GenAIConversationIDKey)
	setStringValueAttr(attrs, e.BlockReason, attr.HookBlockReasonKey)
}

// hookBaseAttrs builds the attributes shared by hook-kind telemetry: the common
// identity attributes, the hook event name/body, hostname, and context.
func hookBaseAttrs[T any](e agentevents.Event[T], eventType types.EventType) (map[attr.Key]any, error) {
	hookName := optionalString(e, types.FieldHookName)
	hookSource := optionalString(e, types.FieldHookSource)

	attrs := commonAttrs(e, hookSource)
	name := firstNonEmpty(hookName, string(eventType))
	attrs[attr.HookEventKey] = name
	attrs[attr.LogBodyKey] = fmt.Sprintf("Hook: %s", name)
	setStringAttr(attrs, e, types.FieldHookHostname, attr.HookHostnameKey)
	addContextAttrs(attrs, e)
	return attrs, nil
}

func addToolAttrs[T any](attrs map[attr.Key]any, e agentevents.Event[T]) error {
	setStringAttr(attrs, e, types.FieldToolName, attr.ToolNameKey)
	setStringAttr(attrs, e, types.FieldToolSource, attr.ToolCallSourceKey)
	setStringAttr(attrs, e, types.FieldModel, attr.GenAIResponseModelKey)

	toolInput, _, err := e.Any(types.FieldToolInput)
	if err != nil {
		return err
	}
	if toolInput != nil {
		attrs[attr.GenAIToolCallArgumentsKey] = marshalToJSON(toolInput)
	}

	toolOutput, _, err := e.Any(types.FieldToolOutput)
	if err != nil {
		return err
	}
	if toolOutput != nil {
		attrs[attr.GenAIToolCallResultKey] = marshalToJSON(toolOutput)
	}

	errValue, _, err := e.Any(types.FieldError)
	if err != nil {
		return err
	}
	if errValue != nil {
		attrs[attr.HookErrorKey] = errValue
	}

	toolCallID := optionalString(e, types.FieldToolCallID)
	if toolCallID != "" {
		attrs[attr.TraceIDKey] = hashToolCallIDToTraceID(toolCallID)
		attrs[attr.GenAIToolCallIDKey] = toolCallID
	}
	return nil
}

func addUsageAttrs[T any](attrs map[attr.Key]any, e agentevents.Event[T]) {
	setStringAttr(attrs, e, types.FieldModel, attr.GenAIResponseModelKey)
	addUsageTokenAttrs(attrs, e)
}

func addUsageTokenAttrs[T any](attrs map[attr.Key]any, e agentevents.Event[T]) {
	setIntAttr(attrs, e, types.FieldUsageInputTokens, attr.GenAIUsageInputTokensKey)
	setIntAttr(attrs, e, types.FieldUsageOutputTokens, attr.GenAIUsageOutputTokensKey)
	setIntAttr(attrs, e, types.FieldUsageCacheReadTokens, attr.GenAIUsageCacheReadInputTokensKey)
	setIntAttr(attrs, e, types.FieldUsageCacheWriteTokens, attr.GenAIUsageCacheCreationInputTokensKey)
}

// usageTelemetryAttrs builds the usage-kind telemetry attributes for assistant
// responses.
func usageTelemetryAttrs[T any](e agentevents.Event[T], source string) (map[attr.Key]any, bool, error) {
	hookName := optionalString(e, types.FieldHookName)
	hookSource := optionalString(e, types.FieldHookSource)

	attrs := commonAttrs(e, hookSource)
	attrs[attr.LogBodyKey] = fmt.Sprintf("%s usage metrics", source)
	attrs[attr.ResourceURNKey] = fmt.Sprintf("%s:usage:metrics", e.Provider())
	if hookName != "" {
		attrs[attr.HookEventKey] = hookName
	}
	addContextAttrs(attrs, e)

	addUsageAttrs(attrs, e)
	return attrs, true, nil
}

func finalizeLogParams[T any](e agentevents.Event[T], attrs map[attr.Key]any) (telemetry.LogParams, error) {
	projectID, err := uuid.Parse(e.Context.ProjectID)
	if err != nil {
		return telemetry.LogParams{}, fmt.Errorf("invalid project ID for agent telemetry: %w", err)
	}

	var toolNameAttr string
	if value, ok := attrs[attr.ToolNameKey]; ok {
		var valid bool
		toolNameAttr, valid = value.(string)
		if !valid {
			return telemetry.LogParams{}, fmt.Errorf("agent telemetry attribute %s resolved to %T, want string", attr.ToolNameKey, value)
		}
	}
	var resourceURN string
	if value, ok := attrs[attr.ResourceURNKey]; ok {
		var valid bool
		resourceURN, valid = value.(string)
		if !valid {
			return telemetry.LogParams{}, fmt.Errorf("agent telemetry attribute %s resolved to %T, want string", attr.ResourceURNKey, value)
		}
	}
	toolName := firstNonEmpty(toolNameAttr, string(e.Provider()))
	return telemetry.LogParams{
		Timestamp: eventTimestamp(e),
		ToolInfo: telemetry.ToolInfo{
			Name:           toolName,
			OrganizationID: e.Context.OrgID,
			ProjectID:      projectID.String(),
			ID:             "",
			URN:            resourceURN,
			DeploymentID:   "",
			FunctionID:     nil,
		},
		Attributes: attrs,
	}, nil
}

func eventTimestamp[T any](e agentevents.Event[T]) time.Time {
	if e.Context.Timestamp.IsZero() {
		return time.Now()
	}
	return e.Context.Timestamp
}
