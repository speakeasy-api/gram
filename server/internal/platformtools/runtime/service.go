package runtime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/memory"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	platformchangelog "github.com/speakeasy-api/gram/server/internal/platformtools/changelog"
	platformchats "github.com/speakeasy-api/gram/server/internal/platformtools/chats"
	platformdeployments "github.com/speakeasy-api/gram/server/internal/platformtools/deployments"
	platformdocs "github.com/speakeasy-api/gram/server/internal/platformtools/docs"
	platformlogs "github.com/speakeasy-api/gram/server/internal/platformtools/logs"
	platformmemory "github.com/speakeasy-api/gram/server/internal/platformtools/memory"
	platformplatform "github.com/speakeasy-api/gram/server/internal/platformtools/platform"
	platformplugins "github.com/speakeasy-api/gram/server/internal/platformtools/plugins"
	platformrisk "github.com/speakeasy-api/gram/server/internal/platformtools/risk"
	platformskills "github.com/speakeasy-api/gram/server/internal/platformtools/skills"
	platformtriggers "github.com/speakeasy-api/gram/server/internal/platformtools/triggers"
	platformusers "github.com/speakeasy-api/gram/server/internal/platformtools/users"
	feedbackrecorder "github.com/speakeasy-api/gram/server/internal/skills/feedback"
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

// WithFileURLMinting enables tools that mint short-lived asset download URLs
// (e.g. platform_slack_get_file_url) by supplying the sealing client and the
// public base URL the minted URLs point at.
func WithFileURLMinting(enc *encryption.Client, serverURL *url.URL) Option {
	return func(c *config) {
		c.deps.Encryption = enc
		c.deps.ServerURL = serverURL
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
			Encryption:       nil,
			ServerURL:        nil,
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

// AssistantSkillTools returns the always-on attached-skill tools.
func AssistantSkillTools(logger *slog.Logger, db *pgxpool.Pool, recorder *feedbackrecorder.Recorder, opts ...platformskills.LoadOption) []platformtools.ExternalTool {
	return []platformtools.ExternalTool{
		{Executor: platformskills.NewLoadTool(logger, db, opts...), RequiredFeature: ""},
		{Executor: platformskills.NewAssistantFeedbackTool(db, recorder), RequiredFeature: ""},
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
// content never reaches the model context. The exclusion and false-positive
// tools mutate project state; the risk service gates them on org admin and
// audits the invoking user as the actor.
func ManagedAssistantRiskTools(riskSvc platformrisk.RiskService, redactionKey string) []platformtools.ExternalTool {
	return []platformtools.ExternalTool{
		{Executor: platformrisk.NewListRiskPoliciesTool(riskSvc), RequiredFeature: ""},
		{Executor: platformrisk.NewListRiskResultsForAgentTool(riskSvc), RequiredFeature: ""},
		{Executor: platformrisk.NewListRiskResultsByChatTool(riskSvc), RequiredFeature: ""},
		{Executor: platformrisk.NewGetRiskPolicyStatusTool(riskSvc), RequiredFeature: ""},
		{Executor: platformrisk.NewGetRiskRuleBreakdownTool(riskSvc), RequiredFeature: ""},
		{Executor: platformrisk.NewListRiskExclusionsTool(riskSvc, redactionKey), RequiredFeature: ""},
		{Executor: platformrisk.NewCreateRiskExclusionTool(riskSvc, redactionKey), RequiredFeature: ""},
		{Executor: platformrisk.NewMarkRiskResultsFalsePositiveTool(riskSvc), RequiredFeature: ""},
		{Executor: platformrisk.NewUnmarkRiskResultsFalsePositiveTool(riskSvc), RequiredFeature: ""},
	}
}

// ManagedAssistantDeploymentsTools returns deployment-introspection tools for
// the project's managed assistant.
func ManagedAssistantDeploymentsTools(deploymentsSvc platformdeployments.DeploymentsService) []platformtools.ExternalTool {
	return []platformtools.ExternalTool{
		{Executor: platformdeployments.NewGetDeploymentLogsTool(deploymentsSvc), RequiredFeature: ""},
	}
}

// ManagedAssistantChangelogTools returns the public-changelog tool for the
// project's managed assistant so it can answer "what's new on the platform"
// questions. The HTTP client must come from a guardian policy so the outbound
// fetch stays within the egress rules.
func ManagedAssistantChangelogTools(httpClient *guardian.HTTPClient) []platformtools.ExternalTool {
	return []platformtools.ExternalTool{
		{Executor: platformchangelog.NewGetChangelogTool(httpClient), RequiredFeature: ""},
	}
}

// ManagedAssistantDocsTools returns the public product-documentation tools for
// the project's managed assistant so it can answer product questions from
// speakeasy.com/docs/ai-control-plane instead of its own priors. The HTTP
// client must come from a guardian policy so the outbound fetch stays within
// the egress rules. Both tools share one client so the page index is fetched
// and cached once rather than per tool.
func ManagedAssistantDocsTools(httpClient *guardian.HTTPClient) []platformtools.ExternalTool {
	client := platformdocs.NewClient(httpClient, platformdocs.DefaultSiteURL)
	return []platformtools.ExternalTool{
		{Executor: platformdocs.NewListDocsTool(client), RequiredFeature: ""},
		{Executor: platformdocs.NewGetDocTool(client), RequiredFeature: ""},
	}
}

// ManagedAssistantSkillsTools returns skill management tools for the project's
// managed assistant. The distribution pair is gated on the same "skills"
// feature as the rest; authorization is enforced downstream by the skills
// service against the assistant owner's grants (skill:write for a plugin
// target, project:write for an assistant target).
func ManagedAssistantSkillsTools(skillsSvc platformskills.SkillsService, insights platformskills.SkillInsightsReader) []platformtools.ExternalTool {
	return []platformtools.ExternalTool{
		{Executor: platformskills.NewCreateTool(skillsSvc), RequiredFeature: "skills"},
		{Executor: platformskills.NewListTool(skillsSvc), RequiredFeature: "skills"},
		{Executor: platformskills.NewGetTool(skillsSvc), RequiredFeature: "skills"},
		{Executor: platformskills.NewListVersionsTool(skillsSvc), RequiredFeature: "skills"},
		{Executor: platformskills.NewListDistributionsTool(skillsSvc), RequiredFeature: "skills"},
		{Executor: platformskills.NewDistributeTool(skillsSvc), RequiredFeature: "skills"},
		{Executor: platformskills.NewUndistributeTool(skillsSvc), RequiredFeature: "skills"},
		{Executor: platformskills.NewInsightsTool(skillsSvc, insights), RequiredFeature: "skills"},
	}
}

// PlatformMCPReadTools returns the Platform MCP read tools re-served to the
// project's managed assistant over the assistant runtime channel. The reader
// is the same Postgres reader backing the OAuth-facing Platform MCP surface,
// so both channels return identical data.
func PlatformMCPReadTools(reader platformmcp.Reader) []platformtools.ExternalTool {
	return []platformtools.ExternalTool{
		{Executor: platformplatform.NewGetPlatformContextTool(), RequiredFeature: ""},
		{Executor: platformplatform.NewListProjectsTool(reader), RequiredFeature: ""},
		{Executor: platformplatform.NewFindMCPTool(reader), RequiredFeature: ""},
		{Executor: platformplatform.NewGetMCPTool(reader), RequiredFeature: ""},
	}
}

// ManagedAssistantPluginsTools returns the plugin catalog tool for the
// project's managed assistant. It exists so the assistant can resolve a plugin
// by name to the ID that platform_distribute_skill needs; without it the only
// plugin IDs in reach are those that already carry a distributed skill.
func ManagedAssistantPluginsTools(pluginsSvc platformplugins.PluginsService) []platformtools.ExternalTool {
	return []platformtools.ExternalTool{
		{Executor: platformplugins.NewListPluginsTool(pluginsSvc), RequiredFeature: ""},
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
			return nil, oops.E(oops.CodeNotFound, nil, "platform tool not found").LogWarn(ctx, s.logger)
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
		return nil, oops.E(oops.CodeNotFound, nil, "platform tool not found").LogWarn(ctx, s.logger)
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
