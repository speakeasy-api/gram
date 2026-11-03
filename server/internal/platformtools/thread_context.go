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
	ctx, env, payload, err := PrepareThreadContext(ctx, t.Descriptor(), env, payload, t.resolve)
	if err != nil {
		return err
	}
	if _, automatic := contextvalues.GetAssistantPrincipal(ctx); !automatic {
		if _, selected := contextvalues.GetAssistantToolTarget(ctx); selected && payload != nil {
			var input map[string]json.RawMessage
			if err := json.NewDecoder(payload).Decode(&input); err != nil {
				return fmt.Errorf("decode prepared tool input: %w", err)
			}
			delete(input, "thread_id")
			body, err := json.Marshal(input)
			if err != nil {
				return fmt.Errorf("encode prepared tool input: %w", err)
			}
			payload = bytes.NewReader(body)
		}
	}
	return callThreadTool(t.PlatformToolExecutor, ctx, env, payload, wr)
}

// PrepareThreadContext resolves explicit tool targeting without changing caller
// authentication. Callers can use the returned context for both execution and
// target attribution. The returned reader preserves the original payload, including
// thread_id, for gateway schema validation. Only the executor wrapper strips it.
// Repeated preparation preserves an already selected target.
func PrepareThreadContext(ctx context.Context, descriptor ToolDescriptor, env toolconfig.ToolCallEnv, payload io.Reader, resolve ThreadContextResolver) (context.Context, toolconfig.ToolCallEnv, io.Reader, error) {
	if _, ok := contextvalues.GetAssistantToolTarget(ctx); ok || descriptor.ThreadContext == "" {
		return ctx, env, payload, nil
	}
	var body []byte
	var err error
	if payload != nil {
		body, err = io.ReadAll(payload)
	}
	if err != nil {
		return ctx, env, nil, fmt.Errorf("read tool input: %w", err)
	}
	var input map[string]json.RawMessage
	if len(body) != 0 {
		if err := json.Unmarshal(body, &input); err != nil {
			return ctx, env, nil, fmt.Errorf("decode tool input: %w", err)
		}
	}
	needed := descriptor.ThreadContext == core.ThreadContextRequired
	switch descriptor.ThreadContext {
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
		return ctx, env, bytes.NewReader(body), nil
	}
	var rawID string
	if err := json.Unmarshal(input["thread_id"], &rawID); err != nil {
		return ctx, env, nil, fmt.Errorf("thread_id is required without an assistant principal")
	}
	threadID, err := uuid.Parse(rawID)
	if err != nil || threadID == uuid.Nil {
		return ctx, env, nil, fmt.Errorf("thread_id must be a non-zero UUID")
	}
	if resolve == nil {
		return ctx, env, nil, fmt.Errorf("explicit thread selection is not configured")
	}
	principal, chatID, err := resolve(ctx, threadID)
	if err != nil {
		return ctx, env, nil, err
	}
	if principal.ThreadID != threadID || principal.AssistantID == uuid.Nil {
		return ctx, env, nil, fmt.Errorf("thread resolver returned invalid context")
	}
	ctx = contextvalues.SetAssistantToolTarget(ctx, principal)
	env.GramChatID = chatID
	return ctx, env, bytes.NewReader(body), nil
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
