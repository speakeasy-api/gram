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
	"github.com/speakeasy-api/gram/server/internal/audit"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type Service struct {
	logger    *slog.Logger
	executors map[string]platformtools.PlatformToolExecutor
}

var _ gateway.PlatformExecutor = (*Service)(nil)

type Option func(*platformtools.Dependencies)

func WithTriggerTools(app *bgtriggers.App) Option {
	return func(deps *platformtools.Dependencies) {
		deps.TriggerApp = app
	}
}

func WithSlackHTTPClient(client *guardian.HTTPClient) Option {
	return func(deps *platformtools.Dependencies) {
		deps.SlackHTTPClient = client
	}
}

func NewService(
	logger *slog.Logger,
	db *pgxpool.Pool,
	telemetrySvc platformtools.TelemetryService,
	auditLogger *audit.Logger,
	options ...Option,
) *Service {
	deps := platformtools.Dependencies{
		Logger:           logger,
		DB:               db,
		TelemetryService: telemetrySvc,
		Audit:            auditLogger,
		TriggerApp:       nil,
		SlackHTTPClient:  nil,
	}
	for _, option := range options {
		option(&deps)
	}

	return &Service{
		logger:    logger.With(attr.SlogComponent("platform_tools")),
		executors: platformtools.BuildExecutors(deps),
	}
}

func (s *Service) ExecuteTool(ctx context.Context, plan *gateway.ToolCallPlan, env toolconfig.ToolCallEnv, requestBody io.Reader) (*gateway.PlatformResult, error) {
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
	if err := executor.Call(ctx, env, requestBody, &out); err != nil {
		return nil, fmt.Errorf("execute platform tool %s: %w", plan.Descriptor.URN, err)
	}

	return &gateway.PlatformResult{
		StatusCode:  http.StatusOK,
		ContentType: "application/json",
		Body:        out.Bytes(),
	}, nil
}
