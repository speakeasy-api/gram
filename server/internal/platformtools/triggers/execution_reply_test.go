package triggers

import (
	"bytes"
	"context"
	"errors"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

type denialNotifier struct{ calls int }

func (n *denialNotifier) NotifyAssistantExecutionDenied(context.Context) error { n.calls++; return nil }
func TestExecutionReplyHasNoModelControlledTargetOrContent(t *testing.T) {
	t.Parallel()
	n := &denialNotifier{}
	tool := NewExecutionDeniedReplyTool(n)
	var out bytes.Buffer
	require.NoError(t, tool.Call(t.Context(), toolconfig.ToolCallEnv{}, strings.NewReader(`{}`), &out))
	require.Equal(t, 1, n.calls)
	require.JSONEq(t, `{"sent":true}`, out.String())
	for _, payload := range []string{`{"text":"private result"}`, `{"channel":"OTHER"}`, `{"user":"OTHER"}`} {
		require.Error(t, tool.Call(t.Context(), toolconfig.ToolCallEnv{}, strings.NewReader(payload), &out))
	}
	require.Equal(t, 1, n.calls)
}

type failingDenialNotifier struct{}

func (failingDenialNotifier) NotifyAssistantExecutionDenied(context.Context) error {
	return errors.New("synthetic confidential provider detail")
}
func TestExecutionReplyEmptyArgumentsAndPrivateFailure(t *testing.T) {
	t.Parallel()
	n := &denialNotifier{}
	tool := NewExecutionDeniedReplyTool(n)
	var out bytes.Buffer
	require.NoError(t, tool.Call(t.Context(), toolconfig.ToolCallEnv{}, strings.NewReader(""), &out))
	require.Equal(t, 1, n.calls)
	require.JSONEq(t, `{"sent":true}`, out.String())
	failed := NewExecutionDeniedReplyTool(failingDenialNotifier{})
	err := failed.Call(t.Context(), toolconfig.ToolCallEnv{}, strings.NewReader(`{}`), &out)
	require.EqualError(t, err, "assistant refusal delivery unavailable")
	require.NotContains(t, err.Error(), "confidential")
}
