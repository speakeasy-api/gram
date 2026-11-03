package platformtools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/stretchr/testify/require"
)

type threadTestExecutor struct {
	descriptor ToolDescriptor
	call       func(context.Context, toolconfig.ToolCallEnv, io.Reader, io.Writer) error
}

func (t threadTestExecutor) Descriptor() ToolDescriptor { return t.descriptor }
func (t threadTestExecutor) Call(ctx context.Context, env toolconfig.ToolCallEnv, r io.Reader, w io.Writer) error {
	return t.call(ctx, env, r, w)
}

func TestGatewayThreadContextSchemas(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	projectID := uuid.New()
	descriptor := core.ToolDescriptor{SourceSlug: "slack", HandlerName: "send_message"}
	toolURN := descriptor.ToolURN().String()
	for _, automatic := range []bool{false, true} {
		callCtx := ctx
		if automatic {
			callCtx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{AssistantID: uuid.New(), ThreadID: uuid.New()})
		}
		listed := ListTypedTools(callCtx, "test-org", projectID, toolURN, nil)
		require.Len(t, listed, 1)
		require.Equal(t, !automatic, strings.Contains(listed[0].PlatformToolDefinition.Schema, `"thread_id"`))
	}
}

func TestThreadFallbackAutomatic(t *testing.T) {
	t.Parallel()
	principal := contextvalues.AssistantPrincipal{AssistantID: uuid.New(), ThreadID: uuid.New()}
	ctx := contextvalues.SetAssistantPrincipal(t.Context(), principal)
	tool := WithThreadFallback(threadTestExecutor{descriptor: ToolDescriptor{ThreadContext: core.ThreadContextRequired}, call: func(ctx context.Context, env toolconfig.ToolCallEnv, r io.Reader, _ io.Writer) error {
		got, ok := contextvalues.GetAssistantPrincipal(ctx)
		require.True(t, ok)
		require.Equal(t, principal, got)
		require.Equal(t, "automatic-chat", env.GramChatID)
		body, err := io.ReadAll(r)
		require.NoError(t, err)
		require.JSONEq(t, `{"thread_id":"ignored"}`, string(body))
		return nil
	}}, func(context.Context, uuid.UUID) (contextvalues.AssistantPrincipal, string, error) {
		t.Fatal("automatic context must not resolve explicit thread")
		return contextvalues.AssistantPrincipal{}, "", nil
	})
	require.NoError(t, tool.Call(ctx, toolconfig.ToolCallEnv{GramChatID: "automatic-chat"}, strings.NewReader(`{"thread_id":"ignored"}`), io.Discard))
}

func TestThreadFallbackExplicit(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	auth := &contextvalues.AuthContext{ProjectID: &projectID}
	ctx := contextvalues.SetAuthContext(t.Context(), auth)
	selected := uuid.New()
	principal := contextvalues.AssistantPrincipal{AssistantID: uuid.New(), ThreadID: selected}
	called := false
	tool := WithThreadFallback(threadTestExecutor{descriptor: ToolDescriptor{ThreadContext: core.ThreadContextRequired}, call: func(ctx context.Context, env toolconfig.ToolCallEnv, r io.Reader, _ io.Writer) error {
		called = true
		gotAuth, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.Same(t, auth, gotAuth)
		got, ok := contextvalues.GetAssistantPrincipal(ctx)
		require.True(t, ok)
		require.Equal(t, principal, got)
		require.Equal(t, "selected-chat", env.GramChatID)
		body, err := io.ReadAll(r)
		require.NoError(t, err)
		require.JSONEq(t, `{"content":"keep"}`, string(body))
		return nil
	}}, func(gotCtx context.Context, id uuid.UUID) (contextvalues.AssistantPrincipal, string, error) {
		require.Equal(t, ctx, gotCtx)
		require.Equal(t, selected, id)
		return principal, "selected-chat", nil
	})
	require.NoError(t, tool.Call(ctx, toolconfig.ToolCallEnv{}, strings.NewReader(`{"thread_id":"`+selected.String()+`","content":"keep"}`), io.Discard))
	require.True(t, called)
	_, exists := contextvalues.GetAssistantPrincipal(ctx)
	require.False(t, exists)
}

func TestThreadFallbackRequiredAndDenied(t *testing.T) {
	t.Parallel()
	denied := errors.New("existing authorization denied")
	tool := WithThreadFallback(threadTestExecutor{descriptor: ToolDescriptor{ThreadContext: core.ThreadContextRequired}, call: func(context.Context, toolconfig.ToolCallEnv, io.Reader, io.Writer) error {
		t.Fatal("must not dispatch")
		return nil
	}}, func(context.Context, uuid.UUID) (contextvalues.AssistantPrincipal, string, error) {
		return contextvalues.AssistantPrincipal{}, "", denied
	})
	for _, body := range []string{`{}`, `{"thread_id":"bad"}`, `{"thread_id":"00000000-0000-0000-0000-000000000000"}`} {
		require.Error(t, tool.Call(t.Context(), toolconfig.ToolCallEnv{}, strings.NewReader(body), io.Discard))
	}
	require.ErrorIs(t, tool.Call(t.Context(), toolconfig.ToolCallEnv{}, strings.NewReader(`{"thread_id":"`+uuid.NewString()+`"}`), io.Discard), denied)
}

func TestThreadFallbackConditional(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		policy core.ThreadContextPolicy
		body   string
		needed bool
	}{
		{"slack self", core.ThreadContextSlackSelf, `{"route_replies":"self"}`, true},
		{"slack new", core.ThreadContextSlackSelf, `{"route_replies":"new"}`, false},
		{"slack default", core.ThreadContextSlackSelf, `{}`, false},
		{"wake", core.ThreadContextWake, `{"input":{"definition_slug":"wake"}}`, true},
		{"other trigger", core.ThreadContextWake, `{"input":{"definition_slug":"slack"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			called := false
			tool := WithThreadFallback(threadTestExecutor{descriptor: ToolDescriptor{ThreadContext: tc.policy}, call: func(context.Context, toolconfig.ToolCallEnv, io.Reader, io.Writer) error { called = true; return nil }}, nil)
			err := tool.Call(t.Context(), toolconfig.ToolCallEnv{}, strings.NewReader(tc.body), io.Discard)
			require.Equal(t, tc.needed, err != nil)
			require.Equal(t, !tc.needed, called)
		})
	}
}

func TestThreadFallbackConcurrent(t *testing.T) {
	t.Parallel()
	tool := WithThreadFallback(threadTestExecutor{descriptor: ToolDescriptor{ThreadContext: core.ThreadContextRequired}, call: func(ctx context.Context, env toolconfig.ToolCallEnv, _ io.Reader, _ io.Writer) error {
		principal, ok := contextvalues.GetAssistantPrincipal(ctx)
		if !ok || env.GramChatID != principal.ThreadID.String() {
			return errors.New("thread context leaked across calls")
		}
		return nil
	}}, func(_ context.Context, id uuid.UUID) (contextvalues.AssistantPrincipal, string, error) {
		return contextvalues.AssistantPrincipal{AssistantID: uuid.New(), ThreadID: id}, id.String(), nil
	})
	for range 24 {
		t.Run(uuid.NewString(), func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			require.NoError(t, tool.Call(ctx, toolconfig.ToolCallEnv{}, strings.NewReader(`{"thread_id":"`+uuid.NewString()+`"}`), io.Discard))
			_, exists := contextvalues.GetAssistantPrincipal(ctx)
			require.False(t, exists)
		})
	}
}

func TestDescriptorForContextConcurrent(t *testing.T) {
	t.Parallel()
	for _, policy := range []core.ThreadContextPolicy{"", core.ThreadContextRequired, core.ThreadContextSlackSelf, core.ThreadContextWake} {
		original := ToolDescriptor{ThreadContext: policy, InputSchema: []byte(`{"type":"object","properties":{"route_replies":{"type":"string"},"input":{"type":"object"}},"additionalProperties":false}`)}
		for i := range 12 {
			t.Run(string(policy)+string(rune('a'+i)), func(t *testing.T) {
				t.Parallel()
				ctx := t.Context()
				automatic := i%2 == 0
				if automatic {
					ctx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{})
				}
				desc := DescriptorForContext(ctx, original)
				var schema jsonschema.Schema
				require.NoError(t, json.Unmarshal(desc.InputSchema, &schema))
				resolved, err := schema.Resolve(nil)
				require.NoError(t, err)
				needed := !automatic && policy != ""
				require.Equal(t, needed, schema.Properties["thread_id"] != nil)
				input := map[string]any{}
				if policy == core.ThreadContextSlackSelf {
					input["route_replies"] = "self"
				}
				if policy == core.ThreadContextWake {
					input["input"] = map[string]any{"definition_slug": "wake"}
				}
				require.Equal(t, needed, resolved.Validate(input) != nil)
				if needed {
					input["thread_id"] = uuid.NewString()
					require.NoError(t, resolved.Validate(input))
				}
				require.NotContains(t, string(original.InputSchema), "thread_id")
			})
		}
	}
}
