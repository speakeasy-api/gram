package platformtools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// ThreadContextResolver resolves an explicitly selected thread under the caller's
// existing authorization. It must not replace the caller's auth context or actor.
// The returned chat ID supplies the same conversation metadata as automatic calls.
type ThreadContextResolver func(context.Context, uuid.UUID) (contextvalues.AssistantPrincipal, string, error)

type threadFallback struct {
	PlatformToolExecutor
	resolve ThreadContextResolver
}

// WithThreadFallback adds explicit thread selection without changing the tool's
// automatic assistant behavior or its authorization checks. Use DescriptorForContext
// when listing the wrapped tool to callers.
func WithThreadFallback(tool PlatformToolExecutor, resolve ThreadContextResolver) PlatformToolExecutor {
	if tool.Descriptor().ThreadContext == "" {
		return tool
	}
	return &threadFallback{PlatformToolExecutor: tool, resolve: resolve}
}

func (t *threadFallback) Call(ctx context.Context, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if _, ok := contextvalues.GetAssistantPrincipal(ctx); ok {
		return callThreadTool(t.PlatformToolExecutor, ctx, env, payload, wr)
	}
	var body []byte
	var err error
	if payload != nil {
		body, err = io.ReadAll(payload)
	}
	if err != nil {
		return fmt.Errorf("read tool input: %w", err)
	}
	var input map[string]json.RawMessage
	if len(body) != 0 {
		if err := json.Unmarshal(body, &input); err != nil {
			return fmt.Errorf("decode tool input: %w", err)
		}
	}
	needed := t.Descriptor().ThreadContext == core.ThreadContextRequired
	switch t.Descriptor().ThreadContext {
	case core.ThreadContextRequired:
		needed = true
	case core.ThreadContextSlackSelf:
		var route string
		_ = json.Unmarshal(input["route_replies"], &route)
		needed = strings.TrimSpace(route) == "self"
	case core.ThreadContextWake:
		var nested struct {
			DefinitionSlug string `json:"definition_slug"`
		}
		_ = json.Unmarshal(input["input"], &nested)
		needed = nested.DefinitionSlug == "wake"
	}
	if !needed {
		return callThreadTool(t.PlatformToolExecutor, ctx, env, bytes.NewReader(body), wr)
	}
	var rawID string
	if err := json.Unmarshal(input["thread_id"], &rawID); err != nil {
		return fmt.Errorf("thread_id is required without an assistant principal")
	}
	threadID, err := uuid.Parse(rawID)
	if err != nil || threadID == uuid.Nil {
		return fmt.Errorf("thread_id must be a non-zero UUID")
	}
	if t.resolve == nil {
		return fmt.Errorf("explicit thread selection is not configured")
	}
	principal, chatID, err := t.resolve(ctx, threadID)
	if err != nil {
		return err
	}
	if principal.ThreadID != threadID || principal.AssistantID == uuid.Nil {
		return fmt.Errorf("thread resolver returned invalid context")
	}
	ctx = contextvalues.SetAssistantPrincipal(ctx, principal)
	env.GramChatID = chatID
	delete(input, "thread_id")
	body, err = json.Marshal(input)
	if err != nil {
		return fmt.Errorf("encode tool input: %w", err)
	}
	return callThreadTool(t.PlatformToolExecutor, ctx, env, bytes.NewReader(body), wr)
}

// DescriptorForContext returns an independent schema for this request. Shared
// descriptors and schemas are never mutated, including across concurrent callers.
func DescriptorForContext(ctx context.Context, descriptor ToolDescriptor) ToolDescriptor {
	if _, ok := contextvalues.GetAssistantPrincipal(ctx); ok || descriptor.ThreadContext == "" {
		return descriptor
	}
	var schema map[string]any
	if err := json.Unmarshal(descriptor.InputSchema, &schema); err != nil {
		panic(fmt.Errorf("decode platform tool schema: %w", err))
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		properties = map[string]any{}
		schema["properties"] = properties
	}
	properties["thread_id"] = map[string]any{"type": "string", "format": "uuid", "description": "Assistant thread ID to use when no automatic assistant context is available."}
	required, _ := schema["required"].([]any)
	switch descriptor.ThreadContext {
	case core.ThreadContextRequired:
		schema["required"] = append(required, "thread_id")
	case core.ThreadContextSlackSelf, core.ThreadContextWake:
		field, value := "route_replies", "self"
		if descriptor.ThreadContext == core.ThreadContextWake {
			field, value = "definition_slug", "wake"
		}
		condition := map[string]any{"properties": map[string]any{field: map[string]any{"const": value}}, "required": []string{field}}
		if descriptor.ThreadContext == core.ThreadContextWake {
			condition = map[string]any{"properties": map[string]any{"input": condition}, "required": []string{"input"}}
		}
		allOf, _ := schema["allOf"].([]any)
		schema["allOf"] = append(allOf, map[string]any{"if": condition, "then": map[string]any{"required": []string{"thread_id"}}})
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Errorf("encode platform tool schema: %w", err))
	}
	descriptor.InputSchema = encoded
	return descriptor
}

func callThreadTool(tool PlatformToolExecutor, ctx context.Context, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if err := tool.Call(ctx, env, payload, wr); err != nil {
		return fmt.Errorf("call platform tool: %w", err)
	}
	return nil
}
