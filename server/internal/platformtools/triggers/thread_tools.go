package triggers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameUnsubscribeThread = "platform_thread_unsubscribe"

type unsubscribeThreadInput struct{}

type unsubscribeThreadResult struct {
	State string `json:"state"`
}

// UnsubscribeThread stops trigger events on the calling assistant's current
// conversation from reaching it until the assistant is addressed again.
type UnsubscribeThread struct {
	app *bgtriggers.App
}

func NewUnsubscribeThreadTool(app *bgtriggers.App) *UnsubscribeThread {
	return &UnsubscribeThread{app: app}
}

func (t *UnsubscribeThread) Descriptor() core.ToolDescriptor {
	readOnly := false
	destructive := false
	idempotent := true
	openWorld := false

	return core.ToolDescriptor{
		SourceSlug:  sourceTriggers,
		HandlerName: "unsubscribe_thread",
		Name:        toolNameUnsubscribeThread,
		Description: "Stop receiving new messages from the current conversation's thread, and from threads routed to this conversation, unless you are mentioned. A later mention subscribes you again and delivers the messages you missed.",
		InputSchema: core.BuildInputSchema[unsubscribeThreadInput](),
		Annotations: triggerToolAnnotations(readOnly, destructive, idempotent, openWorld),
		Managed:     true,
		Variables:   nil,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (t *UnsubscribeThread) Call(ctx context.Context, env toolconfig.ToolCallEnv, _ io.Reader, wr io.Writer) error {
	authCtx, err := requireProjectAuthContext(ctx)
	if err != nil {
		return err
	}
	principal, ok := contextvalues.GetAssistantPrincipal(ctx)
	if !ok {
		return fmt.Errorf("unsubscribing from a thread requires an assistant principal")
	}

	if err := t.app.SetAssistantThreadRouteState(ctx, bgtriggers.AssistantThread{
		ProjectID:   *authCtx.ProjectID,
		AssistantID: principal.AssistantID,
		ThreadID:    principal.ThreadID,
		ChatID:      env.GramChatID,
	}, bgtriggers.ThreadRouteUnsubscribed); err != nil {
		return fmt.Errorf("unsubscribe thread: %w", err)
	}

	if err := json.NewEncoder(wr).Encode(unsubscribeThreadResult{State: string(bgtriggers.ThreadRouteUnsubscribed)}); err != nil {
		return fmt.Errorf("encode unsubscribe thread result: %w", err)
	}
	return nil
}
