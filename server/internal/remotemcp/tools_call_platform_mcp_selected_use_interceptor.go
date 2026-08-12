package remotemcp

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/toolcallobserver"
)

// PlatformMCPSelectedUseInterceptor observes successful Remote MCP tool calls
// for the Platform MCP first-value projection. It is best-effort: failures and
// malformed identity are ignored so it cannot alter the proxied tool response.
type PlatformMCPSelectedUseInterceptor struct {
	recorder toolcallobserver.SuccessRecorder
	identity proxy.ServerIdentity
	now      func() time.Time
}

const selectedUseRecordingTimeout = 5 * time.Second

var _ proxy.ToolsCallResponseInterceptor = (*PlatformMCPSelectedUseInterceptor)(nil)

func NewPlatformMCPSelectedUseInterceptor(recorder toolcallobserver.SuccessRecorder, identity proxy.ServerIdentity) *PlatformMCPSelectedUseInterceptor {
	return &PlatformMCPSelectedUseInterceptor{recorder: recorder, identity: identity, now: time.Now}
}

func (i *PlatformMCPSelectedUseInterceptor) Name() string {
	return "platform-mcp-selected-use"
}

func (i *PlatformMCPSelectedUseInterceptor) InterceptToolsCallResponse(ctx context.Context, call *proxy.ToolsCallResponse) error {
	if i == nil || i.recorder == nil || call == nil || call.Request == nil || call.Request.Params == nil || call.Result == nil || call.Result.IsError {
		return nil
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" || authCtx.UserID == "" || authCtx.ProjectID == nil {
		return nil
	}
	mcpServerID, err := uuid.Parse(i.identity.McpServerID)
	if err != nil || mcpServerID == uuid.Nil {
		return nil
	}
	go func() {
		recordingCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			selectedUseRecordingTimeout,
		)
		defer cancel()
		i.recorder.RecordSuccessfulToolCall(recordingCtx, toolcallobserver.SuccessObservation{
			OrganizationID: authCtx.ActiveOrganizationID,
			UserID:         authCtx.UserID,
			ProjectID:      *authCtx.ProjectID,
			MCPServerID:    mcpServerID,
			ToolName:       call.Request.Params.Name,
			SucceededAt:    i.now().UTC(),
		})
	}()
	return nil
}
