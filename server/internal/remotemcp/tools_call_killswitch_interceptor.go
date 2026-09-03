package remotemcp

import (
	"context"
	"errors"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// ToolsCallKillswitchCheckpoint evaluates one private proxied tools/call
// after trusted request identity and tenant resolution.
type ToolsCallKillswitchCheckpoint interface {
	Evaluate(context.Context, string, string) (killswitches.TransportDisposition, error)
}

// ToolsCallKillswitchInterceptor enforces mcp_tool_execution before any
// forwarding-side policy, mutation, or upstream request.
type ToolsCallKillswitchInterceptor struct {
	checkpoint     ToolsCallKillswitchCheckpoint
	organizationID string
	mcpServerID    string
	logger         *slog.Logger
}

var _ proxy.ToolsCallRequestInterceptor = (*ToolsCallKillswitchInterceptor)(nil)

func NewToolsCallKillswitchInterceptor(checkpoint ToolsCallKillswitchCheckpoint, organizationID, mcpServerID string, logger *slog.Logger) *ToolsCallKillswitchInterceptor {
	return &ToolsCallKillswitchInterceptor{
		checkpoint:     checkpoint,
		organizationID: organizationID,
		mcpServerID:    mcpServerID,
		logger:         logger,
	}
}

func (i *ToolsCallKillswitchInterceptor) Name() string {
	return "tools-call-killswitch"
}

func (i *ToolsCallKillswitchInterceptor) InterceptToolsCallRequest(ctx context.Context, _ *proxy.ToolsCallRequest) error {
	if i.checkpoint == nil {
		i.logInfrastructureFailure(ctx, errors.New("mcp tool-execution checkpoint is unavailable"))
		return proxy.NewKillswitchInfrastructureRejection()
	}

	disposition, err := i.checkpoint.Evaluate(ctx, i.organizationID, i.mcpServerID)
	if err != nil {
		i.logInfrastructureFailure(ctx, err)
	}

	switch disposition.Kind() {
	case killswitches.TransportDispositionContinue:
		if err == nil {
			return nil
		}
	case killswitches.TransportDispositionMatchedDenial:
		note, ok := disposition.ExternalNote()
		if ok && err == nil {
			return proxy.NewKillswitchMatchRejection(note)
		}
	case killswitches.TransportDispositionInfrastructureRejection:
		return proxy.NewKillswitchInfrastructureRejection()
	}

	i.logInfrastructureFailure(ctx, errors.New("invalid mcp tool-execution checkpoint result"))
	return proxy.NewKillswitchInfrastructureRejection()
}

func (i *ToolsCallKillswitchInterceptor) logInfrastructureFailure(ctx context.Context, err error) {
	if i.logger == nil {
		return
	}
	i.logger.ErrorContext(ctx, "mcp tool-execution checkpoint failed", attr.SlogError(err))
}
