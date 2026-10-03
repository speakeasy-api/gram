package triggers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type ExecutionDenialNotifier interface{ NotifyAssistantExecutionDenied(context.Context) error }
type ExecutionDeniedReply struct{ notifier ExecutionDenialNotifier }

func NewExecutionDeniedReplyTool(notifier ExecutionDenialNotifier) *ExecutionDeniedReply {
	return &ExecutionDeniedReply{notifier: notifier}
}
func (t *ExecutionDeniedReply) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{SourceSlug: sourceTriggers, HandlerName: "execution_denied_reply", Name: "platform_assistant_execution_denied", Description: "Privately tell the original Slack sender that business access was denied. Sends only fixed refusal text, never results, arbitrary text, URLs or user-selected targets. Available only for a bound Slack invocation. Takes no arguments.", InputSchema: core.BuildInputSchema[struct{}](), Annotations: triggerToolAnnotations(false, false, false, true), Managed: true, Variables: nil, OwnerKind: nil, OwnerID: nil}
}
func (t *ExecutionDeniedReply) Call(ctx context.Context, _ toolconfig.ToolCallEnv, input io.Reader, output io.Writer) error {
	if t.notifier == nil {
		return fmt.Errorf("assistant denial replies unavailable")
	}
	var args struct{}
	decoder := json.NewDecoder(input)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil {
		return fmt.Errorf("decode denial reply: %w", err)
	}
	if err := t.notifier.NotifyAssistantExecutionDenied(ctx); err != nil {
		return fmt.Errorf("notify execution denial: %w", err)
	}
	if _, err := io.WriteString(output, `{"sent":true}`); err != nil {
		return fmt.Errorf("write denial reply: %w", err)
	}
	return nil
}
