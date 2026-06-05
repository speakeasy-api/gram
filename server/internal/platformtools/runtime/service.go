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
	platforminsights "github.com/speakeasy-api/gram/server/internal/platformtools/insights"
	platformlogs "github.com/speakeasy-api/gram/server/internal/platformtools/logs"
	platformmemory "github.com/speakeasy-api/gram/server/internal/platformtools/memory"
	platformtriggers "github.com/speakeasy-api/gram/server/internal/platformtools/triggers"
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

// ManagedAssistantLogsTools returns the observability tools granted only to a
// project's managed assistant so it can answer "what's happening in my project?"
// questions in the sidebar — the same telemetry catalog the old AI Insights
// copilot exposed (logs, tool calls, chats, users, metrics, attribute keys).
// Scoped to the managed assistant rather than the universal `assistants`
// toolset because non-managed assistants have no dashboard surface for the
// results.
func ManagedAssistantLogsTools(telemetrySvc platformtools.TelemetryService) []platformtools.ExternalTool {
	return []platformtools.ExternalTool{
		{Executor: platformlogs.NewSearchLogsTool(telemetrySvc), RequiredFeature: ""},
		{Executor: platformlogs.NewSearchToolCallsTool(telemetrySvc), RequiredFeature: ""},
		{Executor: platformlogs.NewSearchChatsTool(telemetrySvc), RequiredFeature: ""},
		{Executor: platformlogs.NewSearchUsersTool(telemetrySvc), RequiredFeature: ""},
		{Executor: platformlogs.NewGetProjectMetricsSummaryTool(telemetrySvc), RequiredFeature: ""},
		{Executor: platformlogs.NewGetUserMetricsSummaryTool(telemetrySvc), RequiredFeature: ""},
		{Executor: platformlogs.NewGetObservabilityOverviewTool(telemetrySvc), RequiredFeature: ""},
		{Executor: platformlogs.NewListAttributeKeysTool(telemetrySvc), RequiredFeature: ""},
	}
}

// ManagedAssistantServiceProviders supplies the management services that back
// the managed assistant's non-telemetry observability tools. They are providers
// (func() Service) rather than values because the managed-assistant toolset is
// built early at startup — before some of these services exist — to avoid a
// toolset → mcpService → chatService → toolset construction cycle. Each provider
// is invoked lazily at tool-call time, by which point startup has populated it.
type ManagedAssistantServiceProviders struct {
	Deployments   func() platforminsights.DeploymentsService
	Chat          func() platforminsights.ChatService
	Organizations func() platforminsights.OrganizationsService
	Risk          func() platforminsights.RiskService
}

// ManagedAssistantManagementTools returns the managed assistant's observability
// tools that are backed by management services other than telemetry —
// deployments, chats, organization users, and risk — completing the catalog the
// old client-side AI Insights copilot exposed. Each wrapped method enforces its
// own authz against the calling user's grants (dashboard turns are sender-
// scoped), so e.g. the risk tools resolve only for org admins.
func ManagedAssistantManagementTools(p ManagedAssistantServiceProviders) []platformtools.ExternalTool {
	return []platformtools.ExternalTool{
		{Executor: platforminsights.NewGetDeploymentLogsTool(p.Deployments), RequiredFeature: ""},
		{Executor: platforminsights.NewListChatsTool(p.Chat), RequiredFeature: ""},
		{Executor: platforminsights.NewLoadChatTool(p.Chat), RequiredFeature: ""},
		{Executor: platforminsights.NewListOrganizationUsersTool(p.Organizations), RequiredFeature: ""},
		{Executor: platforminsights.NewListRiskPoliciesTool(p.Risk), RequiredFeature: ""},
		{Executor: platforminsights.NewListRiskResultsForAgentTool(p.Risk), RequiredFeature: ""},
		{Executor: platforminsights.NewListRiskResultsByChatTool(p.Risk), RequiredFeature: ""},
		{Executor: platforminsights.NewGetRiskPolicyStatusTool(p.Risk), RequiredFeature: ""},
	}
}

func (s *Service) ExecuteTool(ctx context.Context, plan *gateway.ToolCallPlan, env toolconfig.ToolCallEnv, requestBody io.Reader) (*gateway.PlatformResult, error) {
	if plan == nil || plan.Kind != gateway.ToolKindPlatform || plan.Descriptor == nil || plan.Platform == nil {
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
