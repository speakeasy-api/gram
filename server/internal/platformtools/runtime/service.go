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
	platformchats "github.com/speakeasy-api/gram/server/internal/platformtools/chats"
	platformdeployments "github.com/speakeasy-api/gram/server/internal/platformtools/deployments"
	platformlogs "github.com/speakeasy-api/gram/server/internal/platformtools/logs"
	platformmemory "github.com/speakeasy-api/gram/server/internal/platformtools/memory"
	platformrisk "github.com/speakeasy-api/gram/server/internal/platformtools/risk"
	platformtriggers "github.com/speakeasy-api/gram/server/internal/platformtools/triggers"
	platformusers "github.com/speakeasy-api/gram/server/internal/platformtools/users"
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
	return []platformtools.ExternalTool{
		{Executor: platformmemory.NewRememberTool(svc), RequiredFeature: ""},
		{Executor: platformmemory.NewRecallTool(svc), RequiredFeature: ""},
		{Executor: platformmemory.NewForgetTool(svc), RequiredFeature: ""},
	}
}

// TriggerExternalTools returns the assistant self-config trigger tools
// (list + configure). Both variants pin target_kind/target_ref to the calling
// assistant principal and strip those fields from the schema so the LLM
// cannot redirect a trigger at a sibling assistant in the same project.
func TriggerExternalTools(db *pgxpool.Pool, app *bgtriggers.App, auditLogger *audit.Logger) []platformtools.ExternalTool {
	return []platformtools.ExternalTool{
		{Executor: platformtriggers.NewAssistantListTriggersTool(db, app), RequiredFeature: ""},
		{Executor: platformtriggers.NewAssistantConfigureTriggerTool(db, app, auditLogger), RequiredFeature: ""},
	}
}

// ManagedAssistantLogsTools returns telemetry-backed observability tools for
// the project's managed assistant. Universal assistants don't get them because
// they have no dashboard surface to display the results.
func ManagedAssistantLogsTools(telemetrySvc platformtools.TelemetryService) []platformtools.ExternalTool {
	return []platformtools.ExternalTool{
		{Executor: platformlogs.NewSearchLogsTool(telemetrySvc), RequiredFeature: ""},
		{Executor: platformlogs.NewSearchToolCallsTool(telemetrySvc), RequiredFeature: ""},
		{Executor: platformlogs.NewGetToolUsageSummaryTool(telemetrySvc), RequiredFeature: ""},
		{Executor: platformlogs.NewSearchChatsTool(telemetrySvc), RequiredFeature: ""},
		{Executor: platformlogs.NewSearchUsersTool(telemetrySvc), RequiredFeature: ""},
		{Executor: platformlogs.NewGetProjectMetricsSummaryTool(telemetrySvc), RequiredFeature: ""},
		{Executor: platformlogs.NewGetUserMetricsSummaryTool(telemetrySvc), RequiredFeature: ""},
		{Executor: platformlogs.NewGetObservabilityOverviewTool(telemetrySvc), RequiredFeature: ""},
		{Executor: platformlogs.NewListAttributeKeysTool(telemetrySvc), RequiredFeature: ""},
	}
}

// ManagedAssistantChatsTools returns chat-history tools for the project's
// managed assistant.
func ManagedAssistantChatsTools(chatSvc platformchats.ChatService) []platformtools.ExternalTool {
	return []platformtools.ExternalTool{
		{Executor: platformchats.NewListChatsTool(chatSvc), RequiredFeature: ""},
		{Executor: platformchats.NewLoadChatTool(chatSvc), RequiredFeature: ""},
	}
}

// ManagedAssistantUsersTools returns user-directory tools for the project's
// managed assistant.
func ManagedAssistantUsersTools(orgSvc platformusers.OrganizationsService) []platformtools.ExternalTool {
	return []platformtools.ExternalTool{
		{Executor: platformusers.NewListOrganizationUsersTool(orgSvc), RequiredFeature: ""},
	}
}

// ManagedAssistantRiskTools returns risk/policy tools for the project's
// managed assistant. listRiskResultsForAgent redacts matches so raw secret
// content never reaches the model context.
func ManagedAssistantRiskTools(riskSvc platformrisk.RiskService) []platformtools.ExternalTool {
	return []platformtools.ExternalTool{
		{Executor: platformrisk.NewListRiskPoliciesTool(riskSvc), RequiredFeature: ""},
		{Executor: platformrisk.NewListRiskResultsForAgentTool(riskSvc), RequiredFeature: ""},
		{Executor: platformrisk.NewListRiskResultsByChatTool(riskSvc), RequiredFeature: ""},
		{Executor: platformrisk.NewGetRiskPolicyStatusTool(riskSvc), RequiredFeature: ""},
	}
}

// ManagedAssistantDeploymentsTools returns deployment-introspection tools for
// the project's managed assistant.
func ManagedAssistantDeploymentsTools(deploymentsSvc platformdeployments.DeploymentsService) []platformtools.ExternalTool {
	return []platformtools.ExternalTool{
		{Executor: platformdeployments.NewGetDeploymentLogsTool(deploymentsSvc), RequiredFeature: ""},
	}
}

func (s *Service) ExecuteTool(ctx context.Context, plan *gateway.ToolCallPlan, env toolconfig.ToolCallEnv, requestBody io.Reader) (*gateway.PlatformResult, error) {
	if plan == nil || plan.Kind != gateway.ToolKindPlatform || plan.Descriptor == nil || plan.Platform == nil {
		return nil, fmt.Errorf("invalid platform tool plan")
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.E(oops.CodeUnauthorized, nil, "platform tool requires project auth context").LogError(ctx, s.logger)
	}
	if authCtx.ProjectID.String() != plan.Descriptor.ProjectID {
		return nil, oops.E(oops.CodeForbidden, nil, "platform tool auth context does not match project").LogError(ctx, s.logger)
	}

	urnStr := plan.Descriptor.URN.String()

	if feature, gated := s.featureGates[urnStr]; gated && s.featureChecker != nil {
		if !s.featureChecker(ctx, authCtx.ActiveOrganizationID, feature) {
			return nil, oops.E(oops.CodeNotFound, nil, "platform tool not found").LogError(ctx, s.logger)
		}
	}

	// A pinned executor wins over the URN registry: scoped variants of a
	// platform tool share a URN, so the caller's match is more specific than
	// what the registry would resolve.
	if plan.Platform.Executor != nil {
		var out bytes.Buffer
		if err := plan.Platform.Executor.Call(ctx, env, requestBody, &out); err != nil {
			return nil, fmt.Errorf("execute platform tool %s: %w", plan.Descriptor.URN, err)
		}
		return &gateway.PlatformResult{
			StatusCode:  http.StatusOK,
			ContentType: "application/json",
			Body:        out.Bytes(),
		}, nil
	}

	executor, ok := s.executors[urnStr]
	if !ok {
		return nil, oops.E(oops.CodeNotFound, nil, "platform tool not found").LogError(ctx, s.logger)
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
