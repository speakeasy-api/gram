package runtime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/memory"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	platformmemory "github.com/speakeasy-api/gram/server/internal/platformtools/memory"
	platformplatform "github.com/speakeasy-api/gram/server/internal/platformtools/platform"
	platformskills "github.com/speakeasy-api/gram/server/internal/platformtools/skills"
	"github.com/speakeasy-api/gram/server/internal/platformtools/threadcontext"
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
	threadAuthorizer *authz.Engine
	deps             platformtools.Dependencies
	extras           []platformtools.ExternalTool
	featureChecker   platformtools.FeatureChecker
}

// WithThreadContextAuthorizer enables explicit thread selection using existing
// caller grants. Automatic assistant calls do not need this option.
func WithThreadContextAuthorizer(engine *authz.Engine) Option {
	return func(c *config) { c.threadAuthorizer = engine }
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
		threadAuthorizer: nil,
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
	for name, executor := range executors {
		executors[name] = platformtools.WithThreadFallback(executor, func(ctx context.Context, threadID uuid.UUID, write bool) (contextvalues.AssistantPrincipal, string, error) {
			return threadcontext.Resolve(ctx, db, cfg.threadAuthorizer, threadID, write)
		})
	}

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
		{Executor: platformmemory.NewRememberTool(svc), RequiredFeature: "", RequiredScope: ""},
		{Executor: platformmemory.NewRecallTool(svc), RequiredFeature: "", RequiredScope: ""},
		{Executor: platformmemory.NewForgetTool(svc), RequiredFeature: "", RequiredScope: ""},
	}
}

// AssistantSkillTools returns the always-on attached-skill tools.
func AssistantSkillTools(logger *slog.Logger, db *pgxpool.Pool, opts ...platformskills.LoadOption) []platformtools.ExternalTool {
	return []platformtools.ExternalTool{
		{Executor: platformskills.NewLoadTool(logger, db, opts...), RequiredFeature: "", RequiredScope: ""},
	}
}

// TriggerExternalTools returns the assistant self-config trigger tools (list +
// configure) and the thread-unsubscribe tool. The trigger tools pin
// target_kind/target_ref to the calling assistant principal and strip those
// fields from the schema so the LLM cannot redirect a trigger at a sibling
// assistant in the same project.
func TriggerExternalTools(db *pgxpool.Pool, app *bgtriggers.App, auditLogger *audit.Logger) []platformtools.ExternalTool {
	return []platformtools.ExternalTool{
		{Executor: platformtriggers.NewAssistantListTriggersTool(db, app), RequiredFeature: "", RequiredScope: ""},
		{Executor: platformtriggers.NewAssistantConfigureTriggerTool(db, app, auditLogger), RequiredFeature: "", RequiredScope: ""},
		{Executor: platformtriggers.NewUnsubscribeThreadTool(app), RequiredFeature: "", RequiredScope: ""},
	}
}

// PlatformMCPReadTools returns the Platform MCP read tools re-served to the
// project's managed assistant over the assistant runtime channel. The reader
// is the same Postgres reader backing the OAuth-facing Platform MCP surface,
// so both channels return identical data.
func PlatformMCPReadTools(reader platformmcp.Reader) []platformtools.ExternalTool {
	return []platformtools.ExternalTool{
		{Executor: platformplatform.NewGetPlatformContextTool(), RequiredFeature: "", RequiredScope: ""},
		{Executor: platformplatform.NewListProjectsTool(reader), RequiredFeature: "", RequiredScope: ""},
		{Executor: platformplatform.NewFindMCPTool(reader), RequiredFeature: "", RequiredScope: ""},
		{Executor: platformplatform.NewGetMCPTool(reader), RequiredFeature: "", RequiredScope: ""},
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
