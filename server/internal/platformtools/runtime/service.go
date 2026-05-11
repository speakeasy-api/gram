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
	"github.com/speakeasy-api/gram/server/internal/memory"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	platformmemory "github.com/speakeasy-api/gram/server/internal/platformtools/memory"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type Service struct {
	logger         *slog.Logger
	executors      map[string]platformtools.PlatformToolExecutor
	featureGates   map[string]string
	featureChecker platformtools.FeatureChecker
}

var _ gateway.PlatformExecutor = (*Service)(nil)

type Option func(*config)

type config struct {
	deps           platformtools.Dependencies
	extras         []platformtools.ExternalTool
	featureChecker platformtools.FeatureChecker
}

func WithTriggerTools(app *bgtriggers.App) Option {
	return func(c *config) {
		c.deps.TriggerApp = app
	}
}

func WithSlackHTTPClient(client *guardian.HTTPClient) Option {
	return func(c *config) {
		c.deps.SlackHTTPClient = client
	}
}

func WithExternalTools(extras []platformtools.ExternalTool) Option {
	return func(c *config) {
		c.extras = extras
	}
}

// WithFeatureChecker gates ExecuteTool dispatch on a per-organization feature
// flag. A nil checker grants every gated tool.
func WithFeatureChecker(checker platformtools.FeatureChecker) Option {
	return func(c *config) {
		c.featureChecker = checker
	}
}

func NewService(
	logger *slog.Logger,
	db *pgxpool.Pool,
	telemetrySvc platformtools.TelemetryService,
	auditLogger *audit.Logger,
	options ...Option,
) *Service {
	cfg := config{
		deps: platformtools.Dependencies{
			Logger:           logger,
			DB:               db,
			TelemetryService: telemetrySvc,
			Audit:            auditLogger,
			TriggerApp:       nil,
			SlackHTTPClient:  nil,
		},
		extras:         nil,
		featureChecker: nil,
	}
	for _, option := range options {
		option(&cfg)
	}

	executors, gates := platformtools.BuildExecutors(cfg.deps, cfg.extras...)

	return &Service{
		logger:         logger.With(attr.SlogComponent("platform_tools")),
		executors:      executors,
		featureGates:   gates,
		featureChecker: cfg.featureChecker,
	}
}

// MemoryExternalTools returns the assistant-memory platform tools wired with
// svc. Pass the same slice to every consumer so dispatch and listing share
// one set of executor instances.
func MemoryExternalTools(svc *memory.MemoryService) []platformtools.ExternalTool {
	feature := string(productfeatures.FeatureAssistantMemory)
	return []platformtools.ExternalTool{
		{Executor: platformmemory.NewRememberTool(svc), RequiredFeature: feature},
		{Executor: platformmemory.NewRecallTool(svc), RequiredFeature: feature},
		{Executor: platformmemory.NewForgetTool(svc), RequiredFeature: feature},
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

	urnStr := plan.Descriptor.URN.String()

	if feature, gated := s.featureGates[urnStr]; gated && s.featureChecker != nil {
		if !s.featureChecker(ctx, authCtx.ActiveOrganizationID, feature) {
			return nil, oops.E(oops.CodeNotFound, nil, "platform tool not found").Log(ctx, s.logger)
		}
	}

	executor, ok := s.executors[urnStr]
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
