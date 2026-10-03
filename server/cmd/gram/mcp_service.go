package gram

import (
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
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/killswitches/mcptoolexecution"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/oktaresourceconnections"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/rag"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
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
	ThreadRouter           *bgtriggers.ThreadRouter
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
	IDTokenKeys            *jwks.KeyResolver
	CallbackOrigins        remotesessions.CallbackOrigins
	CallerAssertions       *mcpauthz.Issuer
}

func newMCPService(c *cli.Context, d mcpServiceDependencies) *mcp.Service {
	cacheImpl := cache.NewRedisCacheAdapter(d.Redis)
	checkpoint := mcptoolexecution.NewCheckpoint(d.DB, mcptoolexecution.DefaultEvaluationTimeout, d.Meter, d.Logger)
	proxy := remotemcp.NewProxyManager(d.Logger, d.Tracer, d.Meter, d.DB, d.Guardian, d.Authz, d.Posthog, d.Telemetry, d.Billing, d.BillingTracker,
		mcpservers.NewToolDispositionCache(d.Logger, d.DB, cacheImpl), platformmcp.NewSelectedUseRecorder(d.DB), toolfilter.NewSessionToolWitnessStore(d.Logger, cacheImpl), checkpoint, d.MCPRisk)
	cidrs := c.StringSlice("tunnel-gateway-cidr-blocks")
	for _, cidr := range cidrs {
		_, _, err := net.ParseCIDR(cidr)
		inv.Require("tunnel gateway CIDR blocks", cidr+" parses", err)
	}
	delegation := remotesessions.NewDelegationService(d.DB, d.Encryption, d.Challenges)
	// Identity-assertion grants are verified against the trusted IdP's keys
	// under the same fetch budgets as its ID tokens.
	chainer := identitychaining.New(d.Logger, d.DB, d.Encryption, d.Challenges, delegation, d.IDTokenKeys, cacheImpl)
	chainer.SetObserver(oktaresourceconnections.NewObserver(d.Logger, d.Meter, d.DB, d.Audit))
	service := mcp.NewService(d.Logger, d.Tracer, d.Meter, d.DB, d.Sessions, d.ChatSessions, d.Environment, d.Posthog, d.Features, d.ServerURL, d.SiteURL, d.Encryption, cacheImpl,
		d.Guardian, d.Functions, d.BillingTracker, d.Billing, d.Telemetry, d.TelemetryService, d.RAG, d.ThreadRouter, d.Authz, d.AssistantTokens, d.ShadowMCP, d.Audit, d.PlatformExtras, d.PlatformFeatureChecker, d.PlatformToolsets,
		d.Identity, usersessions.NewSigner(c.String(usersessions.JWTSigningKeyFlag)), d.Challenges, mcp.NewFederatedDelegationConsumer(delegation), chainer, d.MCPRisk, proxy, route.NewRedis(d.Redis), c.String("tunnel-forward-token"), cidrs, d.CallerAssertions, d.Redis,
		mcp.TunnelPublicConfig{SessionTTL: 0, LiveSessionCap: c.Int("public-tunnels-live-session-cap"), InitializeRate: ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0}, RequestRate: ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0}, MaxRequestLifetime: 0},
		mcp.MetaRuntimeConfig{MemberCallTimeout: c.Duration("meta-member-call-timeout"), ValidationTimeout: 0, AutoVerifyWait: 0, RecheckInterval: c.Duration("remote-session-recheck-interval")})
	service.SetCallbackOrigins(d.CallbackOrigins)
	return service
}
