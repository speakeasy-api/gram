package gram

import (
	"fmt"
	"log/slog"
	"net"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/killswitches/mcptoolexecution"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/rag"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/speakeasy-api/gram/tunnel/route"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type mcpServiceDependencies struct {
	Logger                 *slog.Logger
	Tracer                 trace.TracerProvider
	Meter                  metric.MeterProvider
	DB                     *pgxpool.Pool
	Redis                  *redis.Client
	Sessions               *sessions.Manager
	ChatSessions           *chatsessions.Manager
	Environment            toolconfig.EnvironmentLoader
	Posthog                *posthog.Posthog
	Features               feature.Provider
	ServerURL              *url.URL
	SiteURL                *url.URL
	Encryption             *encryption.Client
	Guardian               *guardian.Policy
	Functions              functions.ToolCaller
	BillingTracker         billing.Tracker
	Billing                billing.Repository
	Telemetry              *tm.Logger
	TelemetryService       *tm.Service
	RAG                    *rag.ToolsetVectorStore
	Triggers               *bgtriggers.App
	Authz                  *authz.Engine
	AssistantTokens        *assistanttokens.Manager
	ShadowMCP              *shadowmcp.Client
	MCPRisk                *mcpriskscan.Evaluator
	Audit                  *audit.Logger
	PlatformExtras         []platformtools.ExternalTool
	PlatformFeatureChecker platformtools.FeatureChecker
	PlatformToolsets       map[string]platformtools.Toolset
	Identity               mcp.IdentityResolver
	Challenges             *remotesessions.ChallengeManager
	CallerAssertions       *mcpauthz.Issuer
}

func newMCPService(c *cli.Context, d mcpServiceDependencies) (*mcp.Service, error) {
	cacheImpl := cache.NewRedisCacheAdapter(d.Redis)
	checkpoint, err := mcptoolexecution.NewCheckpoint(d.DB, mcptoolexecution.DefaultEvaluationTimeout, d.Meter, d.Logger)
	if err != nil {
		return nil, fmt.Errorf("initialize mcp tool-execution checkpoint: %w", err)
	}
	proxy := remotemcp.NewProxyManager(d.Logger, d.Tracer, d.Meter, d.DB, d.Guardian, d.Authz, d.Posthog, d.Telemetry, d.Billing, d.BillingTracker,
		mcpservers.NewToolDispositionCache(d.Logger, d.DB, cacheImpl), platformmcp.NewSelectedUseRecorder(d.DB), toolfilter.NewSessionToolWitnessStore(d.Logger, cacheImpl), checkpoint, d.MCPRisk, d.CallerAssertions)
	cidrs := c.StringSlice("tunnel-gateway-cidr-blocks")
	for _, cidr := range cidrs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return nil, fmt.Errorf("invalid tunnel gateway CIDR block %q: %w", cidr, err)
		}
	}
	service, err := mcp.NewService(d.Logger, d.Tracer, d.Meter, d.DB, d.Sessions, d.ChatSessions, d.Environment, d.Posthog, d.Features, d.ServerURL, d.SiteURL, d.Encryption, cacheImpl,
		d.Guardian, d.Functions, d.BillingTracker, d.Billing, d.Telemetry, d.TelemetryService, d.RAG, d.Triggers, d.Authz, d.AssistantTokens, d.ShadowMCP, d.Audit, d.PlatformExtras, d.PlatformFeatureChecker, d.PlatformToolsets,
		d.Identity, usersessions.NewSigner(c.String(usersessions.JWTSigningKeyFlag)), d.Challenges, d.MCPRisk, proxy, route.NewRedis(d.Redis), c.String("tunnel-forward-token"), cidrs, d.Redis,
		mcp.TunnelPublicConfig{SessionTTL: 0, LiveSessionCap: c.Int("public-tunnels-live-session-cap"), InitializeRate: ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0}, RequestRate: ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0}, MaxRequestLifetime: 0},
		mcp.MetaRuntimeConfig{MemberCallTimeout: c.Duration("meta-member-call-timeout"), ValidationTimeout: 0, AutoVerifyWait: 0, RecheckInterval: c.Duration("remote-session-recheck-interval")})
	if err != nil {
		return nil, fmt.Errorf("initialize MCP service: %w", err)
	}
	service.SetFederatedLoginConsumer(mcp.NewFederatedDelegationConsumer(remotesessions.NewDelegationService(d.DB, d.Encryption, d.Challenges)))
	return service, nil
}
