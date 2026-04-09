package runtime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
)

type Service struct {
	logger    *slog.Logger
	executors map[string]platformtools.PlatformToolExecutor
}

var _ gateway.PlatformExecutor = (*Service)(nil)

func NewService(logger *slog.Logger, _ *pgxpool.Pool, telemetrySvc platformtools.TelemetryService) *Service {
	return &Service{
		logger:    logger.With(attr.SlogComponent("platform_tools")),
		executors: platformtools.BuildExecutors(telemetrySvc),
	}
}

func (s *Service) ExecuteTool(ctx context.Context, plan *gateway.ToolCallPlan, requestBody io.Reader) (*gateway.PlatformResult, error) {
	if plan == nil || plan.Kind != gateway.ToolKindPlatform || plan.Descriptor == nil {
		return nil, fmt.Errorf("invalid platform tool plan")
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.E(oops.CodeUnauthorized, nil, "platform tool requires project auth context").Log(ctx, s.logger)
	}
	if authCtx.ProjectID.String() != plan.Descriptor.ProjectID {
		return nil, oops.E(oops.CodeForbidden, nil, "platform tool auth context does not match project").Log(ctx, s.logger)
	}

	executor, ok := s.executors[plan.Descriptor.URN.String()]
	if !ok {
		return nil, oops.E(oops.CodeNotFound, nil, "platform tool not found").Log(ctx, s.logger)
	}

	var out bytes.Buffer
	if err := executor.Call(ctx, requestBody, &out); err != nil {
		return nil, fmt.Errorf("execute platform tool %s: %w", plan.Descriptor.URN, err)
	}

	return &gateway.PlatformResult{
		StatusCode:  http.StatusOK,
		ContentType: "application/json",
		Body:        out.Bytes(),
	}, nil
}
