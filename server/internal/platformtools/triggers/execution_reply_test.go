package triggers

import (
	"bytes"
	"context"
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
