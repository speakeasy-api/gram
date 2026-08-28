package mcp

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/rag"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/temporal"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	auth_repo "github.com/speakeasy-api/gram/server/internal/auth/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomains_repo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	deployments_repo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	externalmcp_repo "github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/httpcache"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/killswitches/mcptoolexecution"
	"github.com/speakeasy-api/gram/server/internal/mcp/httpheaders"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcp/sessionclientinfo"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata"
	metadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	oauth_repo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizations_repo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	platformtoolsruntime "github.com/speakeasy-api/gram/server/internal/platformtools/runtime"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
	"github.com/speakeasy-api/gram/server/internal/usersessions/clientauth"
	"github.com/speakeasy-api/gram/tunnel/route"
)

// IdentityResolver abstracts the identity operations the authn-challenge OAuth
// flow needs. Satisfied by *identity.Resolver.
type IdentityResolver interface {
	BuildAuthorizationURL(ctx context.Context, params identity.AuthorizationURLParams) (*url.URL, error)
	ExchangeCodeForTokens(ctx context.Context, code string) (*identity.IDPUserInfo, error)
	UpsertUserFromIDP(ctx context.Context, idpUser *identity.IDPUserInfo) (string, error)
	HasAccessToOrganization(ctx context.Context, organizationID, userID string) (*sessions.Organization, string, bool)
}

type Service struct {
	logger                    *slog.Logger
	tracer                    trace.Tracer
	metrics                   *mcpmetrics.Metrics
	identityCoverage          *mcptoolexecution.IdentityCoverageCheckpoint
	hostedToolsCallCheckpoint *mcptoolexecution.HostedCheckpoint
	guardianPolicy            *guardian.Policy
	db                        *pgxpool.Pool
	authRepo                  *auth_repo.Queries
	toolsetsRepo              *toolsets_repo.Queries
	mcpMetadataRepo           *metadata_repo.Queries
	orgsRepo                  *organizations_repo.Queries
	auth                      *auth.Auth
	env                       toolconfig.EnvironmentLoader
	serverURL                 *url.URL
	siteURL                   *url.URL
	posthog                   *posthog.Posthog // posthog metrics will no-op if the dependency is not provided
	// features resolves flag-controlled behavior (the managed assistant's
	// Platform MCP toolset variant). Wired from the environment-aware
	// provider: the posthog client in production, the CSV-backed in-memory
	// provider in local development, feature.InMemory in tests.
	features feature.Provider
	// cimdResolver fetches + validates Client ID Metadata Documents for
	// URL-shaped client_ids and owns the cimd.fetch.* telemetry.
	cimdResolver *cimd.Resolver
	// cimdAdmissionMetrics records the per-issuer admission decisions made
	// before the resolver runs, on their own cimd.admission.decisions
	// instrument (a denial performs no fetch, so it has no place under
	// cimd.fetch.attempts).
	cimdAdmissionMetrics *admission.Metrics
	// clientAssertionVerifier verifies private_key_jwt client assertions at
	// the token and revocation endpoints. Nil without Redis, in which case
	// assertion clients are refused rather than admitted unverified.
	clientAssertionVerifier *clientauth.Verifier
	toolProxy               *gateway.ToolProxy
	oauthRepo               *oauth_repo.Queries
	billingTracker          billing.Tracker
	billingRepository       billing.Repository
	toolsetCache            cache.TypedCacheObject[mv.ToolsetBaseContents]
	telemLogger             *tm.Logger
	vectorToolStore         *rag.ToolsetVectorStore
	temporal                *temporal.Environment
	assistantTokens         *assistanttokens.Manager
	sessions                *sessions.Manager
	identityResolver        IdentityResolver
	chatSessionsManager     *chatsessions.Manager
	externalmcpRepo         *externalmcp_repo.Queries
	deploymentsRepo         *deployments_repo.Queries
	enc                     *encryption.Client
	authz                   *authz.Engine
	shadowMCPClient         *shadowmcp.Client
	auditLogger             *audit.Logger
	platformExtras          []platformtools.ExternalTool
	platformFeatureChecker  platformtools.FeatureChecker
	platformToolsets        map[string]platformtools.Toolset
	authnChallengeCache     cache.TypedCacheObject[AuthnChallengeState]
	userSessionGrantCache   cache.TypedCacheObject[UserSessionGrant]
	// userSessionRefreshReplayCache retains the encrypted rotation outcome.
	userSessionRefreshReplayCache cache.TypedCacheObject[userSessionRefreshReplay]

	// userSessionRefreshReplayCoordination elects the database rotation winner.
	userSessionRefreshReplayCoordination cache.Cache
	toolSelectionCache                   cache.TypedCacheObject[sessionToolSelectionEntry]
	// consentToolInventoryCache holds per-(state, attempt) tool inventory
	// snapshots captured by the consent MCP transport.
	consentToolInventoryCache cache.TypedCacheObject[consentToolInventory]
	// sessionClientInfo holds the MCP client identity captured at initialize
	// so tools/call on the same session can resolve it. Always usable: without
	// Redis it records nothing and every caller resolves as unknown.
	sessionClientInfo *sessionclientinfo.Store
	// userSessionSigner mints the SessionClaims JWT issued at /token.
	// HS256 with GRAM_JWT_SIGNING_KEY -- same key the chat-session signer
	// uses, intentionally separate signer code so each path is removable
	// in isolation.
	userSessionSigner *sessiontokens.Signer
	// remoteChallengeMgr drives the per-remote OAuth authn leg used by the
	// interactive /connect cards and the /remote_login_callback handler.
	remoteChallengeMgr *remotesessions.ChallengeManager
	// remoteProxyManager builds configured remotemcp proxies wired with the
	// MCP-aware interceptor stack. Only consulted by ServeMCPEndpoint's
	// remote-backed branch; may be nil in non-HTTP contexts (e.g. the
	// Temporal worker, which constructs *Service for its programmatic
	// helpers but never serves a runtime request).
	remoteProxyManager *remotemcp.ProxyManager
	tunnelManager      *tunnelManager
	// Nil when no Redis was wired; every public tunneled request then fails closed.
	tunnelPublic *tunnelPublicRuntime

	// metaRuntime bounds the gateway's per-member upstream work.
	metaRuntime MetaRuntimeConfig
}

// oauthTokenInputs is one upstream OAuth access token collected during MCP
// request setup, paired with the tool security schemes it may satisfy.
// ServeToolsetResolved gathers these from the request's auth sources and tool
// dispatch (rpc_tools_call.go / rpc_tools_list.go) injects each token into the
// tools whose oauth2 / openIdConnect security scheme it covers.
type oauthTokenInputs struct {
	// remoteSessionIssuerID identifies the remote_session_issuer whose
	// upstream this token authorizes, when the token came from the
	// issuer-gated remote-session resolver. Invalid for tokens that aren't
	// remote-session-backed. Dispatch selects a token by securityKeys; the
	// issuer id is carried for per-tool routing (AIS-152), which will
	// populate securityKeys from it.
	remoteSessionIssuerID uuid.NullUUID

	// securityKeys lists the tool security-scheme keys this token satisfies.
	// Empty means the token applies to every matching oauth2 / openIdConnect
	// tool on the server (one token covering the whole server); when
	// populated, dispatch injects the token only into tools whose security
	// scheme key is in the list.
	securityKeys []string

	// Token is the upstream bearer access token value. Dispatch writes it into
	// the matching tool's *_ACCESS_TOKEN env var, which the gateway forwards
	// as the Authorization header on the outgoing upstream request.
	Token string
}

// appendRemoteSessionTokenInputs converts a remote_session_issuer_id -> token
// map (resolved by ApplyIssuerGate / ResolveAccessTokens) into oauthTokenInputs
// entries, tagging each with its remote_session_issuer_id. securityKeys is
// left empty, so dispatch injects a remote-session token into every matching
// oauth2 tool — correct only when a single remote issuer is bound.
//
// Fails closed when more than one token resolves: nothing maps a tool's
// security scheme to a remote_session_issuer (AGE-3285), so we cannot tell
// which tool needs which issuer's token, and injecting all of them with empty
// securityKeys would forward an arbitrary bearer upstream. This mirrors
// routeUpstreamToken's fail-closed posture for the proxied-MCP backends,
// which can route by the credential's grant-time resource — toolset dispatch
// has no equivalent qualified identity yet. The multi-token state is
// reachable: the one_per_issuer index that used to cap a user_session_issuer
// at one client was dropped in AIS-137.
func appendRemoteSessionTokenInputs(dst []oauthTokenInputs, tokens map[uuid.UUID]remotesessions.UpstreamToken) ([]oauthTokenInputs, error) {
	if len(tokens) > 1 {
		return nil, fmt.Errorf("issuer-gated endpoint resolved %d remote-session upstream tokens; per-tool routing requires a security-scheme-to-issuer mapping (AGE-3285)", len(tokens))
	}
	for issuerID, entry := range tokens {
		// Defensive: ResolveAccessTokens never maps an issuer to an empty
		// token (it returns ErrNoValidToken instead), so this skip should not
		// fire; it guards against a caller passing an empty-valued entry.
		if entry.Token == "" {
			continue
		}
		dst = append(dst, oauthTokenInputs{
			securityKeys:          nil,
			remoteSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true},
			Token:                 entry.Token,
		})
	}
	return dst, nil
}

type mcpInputs struct {
	projectID        uuid.UUID
	organizationID   string
	toolset          string
	environment      string
	mcpEnvVariables  map[string]string
	oauthTokenInputs []oauthTokenInputs
	authenticated    bool
	sessionID        string
	chatID           string
	mode             ToolMode
	userID           string
	externalUserID   string
	apiKeyID         string
	// toolVariationsGroupID is the effective variation group resolved per
	// request (mcp_servers, then toolsets, then nil for the project default).
	toolVariationsGroupID *uuid.UUID
	// skipProxyTools drops external-MCP passthrough tools from dispatch.
	// The meta surface sets it: those tools are hidden from its describe
	// catalog, so execute must not reach them through the hosted path either.
	skipProxyTools bool
	// mcpServerID is the fronting mcp_servers row id when the request arrived
	// via an mcp_endpoint. Nil on the legacy toolset-by-slug path and for
	// internal (agent-workflow) callers, which have no fronting server.
	mcpServerID *uuid.UUID
	// tags is the parsed ?tags= filter. When non-empty, tools/list and
	// tools/call expose only tools whose variation row carries one of these
	// tags. Empty means no filtering.
	tags []string
	// protocolVersion is the protocol revision resolved for this request:
	// what the request declared (header, falling back to per-request `_meta`)
	// for telemetry, and the supported-set member in effect for behavior.
	// Threaded here because the dispatch and handlers run without access to
	// the *http.Request. Resolved once where this struct is built; the
	// initialize handler overwrites InEffect with the negotiated answer, which
	// is the one sanctioned mutation.
	protocolVersion mcpversions.Resolution
	// identityCoverageRecorded prevents internal dispatch layers from
	// recounting a request already observed at the method boundary.
	identityCoverageRecorded bool
	// toolSelection is the consent-screen tool policy loaded from the
	// session row by the issuer gate. Nil means all tools; non-nil is always
	// restrictive and intersects with the live toolset, ?tags=, and RBAC.
	toolSelection *toolfilter.SessionSelection
}

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	chatSessionsManager *chatsessions.Manager,
	env toolconfig.EnvironmentLoader,
	posthog *posthog.Posthog,
	features feature.Provider,
	serverURL *url.URL,
	siteURL *url.URL,
	enc *encryption.Client,
	cacheImpl cache.Cache,
	guardianPolicy *guardian.Policy,
	funcCaller functions.ToolCaller,
	billingTracker billing.Tracker,
	billingRepository billing.Repository,
	telemLogger *tm.Logger,
	telemSvc *tm.Service,
	vectorToolStore *rag.ToolsetVectorStore,
	triggerApp *bgtriggers.App,
	temporal *temporal.Environment,
	authzEngine *authz.Engine,
	assistantTokens *assistanttokens.Manager,
	shadowMCPClient *shadowmcp.Client,
	auditLogger *audit.Logger,
	platformExtras []platformtools.ExternalTool,
	platformFeatureChecker platformtools.FeatureChecker,
	platformToolsets map[string]platformtools.Toolset,
	identityResolver IdentityResolver,
	userSessionSigner *sessiontokens.Signer,
	remoteChallengeMgr *remotesessions.ChallengeManager,
	remoteProxyManager *remotemcp.ProxyManager,
	tunnelRoutes route.Store,
	tunnelForwardToken string,
	tunnelGatewayCIDRs []string,
	redisClient *redis.Client,
	tunnelPublicConfig TunnelPublicConfig,
	metaRuntimeConfig MetaRuntimeConfig,
) (*Service, error) {
	tracer := tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/mcp")
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/mcp")
	logger = logger.With(attr.SlogComponent("mcp"))
	metrics := mcpmetrics.NewMetrics(meter, logger)
	hostedToolsCallCheckpoint, err := mcptoolexecution.NewHostedCheckpoint(db, meterProvider, logger, metrics)
	if err != nil {
		return nil, fmt.Errorf("initialize hosted MCP kill-switch checkpoint: %w", err)
	}

	platformSvc := platformtoolsruntime.NewService(
		logger,
		db,
		telemSvc,
		auditLogger,
		platformtoolsruntime.WithTriggerTools(triggerApp),
		platformtoolsruntime.WithSlackHTTPClient(guardianPolicy.PooledClient()),
		platformtoolsruntime.WithExternalTools(platformExtras),
		platformtoolsruntime.WithFeatureChecker(platformFeatureChecker),
	)

	return &Service{
		logger:                    logger,
		tracer:                    tracer,
		metrics:                   metrics,
		identityCoverage:          mcptoolexecution.NewIdentityCoverageCheckpoint(db, metrics),
		hostedToolsCallCheckpoint: hostedToolsCallCheckpoint,
		guardianPolicy:            guardianPolicy,
		db:                        db,
		authRepo:                  auth_repo.New(db),
		toolsetsRepo:              toolsets_repo.New(db),
		mcpMetadataRepo:           metadata_repo.New(db),
		orgsRepo:                  organizations_repo.New(db),
		deploymentsRepo:           deployments_repo.New(db),
		externalmcpRepo:           externalmcp_repo.New(db),
		auth:                      auth.New(logger, db, sessions, authzEngine),
		env:                       env,
		serverURL:                 serverURL,
		siteURL:                   siteURL,
		posthog:                   posthog,
		features:                  features,
		cimdResolver:              cimd.NewResolver(guardianPolicy, meterProvider, logger),
		cimdAdmissionMetrics:      admission.NewMetrics(meterProvider, logger),
		clientAssertionVerifier:   newClientAssertionVerifier(redisClient, guardianPolicy, meterProvider, logger),
		toolProxy: gateway.NewToolProxy(
			logger,
			tracerProvider,
			meterProvider,
			gateway.ToolCallSourceMCP,
			enc,
			cacheImpl,
			guardianPolicy,
			funcCaller,
			platformSvc,
		),
		oauthRepo:              oauth_repo.New(db),
		billingTracker:         billingTracker,
		billingRepository:      billingRepository,
		toolsetCache:           cache.NewTypedObjectCache[mv.ToolsetBaseContents](logger.With(attr.SlogCacheNamespace("toolset")), cacheImpl, cache.SuffixNone),
		telemLogger:            telemLogger,
		vectorToolStore:        vectorToolStore,
		temporal:               temporal,
		assistantTokens:        assistantTokens,
		sessions:               sessions,
		chatSessionsManager:    chatSessionsManager,
		enc:                    enc,
		authz:                  authzEngine,
		shadowMCPClient:        shadowMCPClient,
		auditLogger:            auditLogger,
		platformExtras:         platformExtras,
		platformFeatureChecker: platformFeatureChecker,
		platformToolsets:       platformToolsets,
		authnChallengeCache: cache.NewTypedObjectCache[AuthnChallengeState](
			logger.With(attr.SlogCacheNamespace("authn_challenge")),
			cacheImpl,
			cache.SuffixNone,
		),
		userSessionGrantCache: cache.NewTypedObjectCache[UserSessionGrant](
			logger.With(attr.SlogCacheNamespace("user_session_grant")),
			cacheImpl,
			cache.SuffixNone,
		),
		userSessionRefreshReplayCache: cache.NewTypedObjectCache[userSessionRefreshReplay](
			logger.With(attr.SlogCacheNamespace("user_session_refresh_replay")),
			cacheImpl,
			cache.SuffixNone,
		),
		userSessionRefreshReplayCoordination: cacheImpl,
		toolSelectionCache: cache.NewTypedObjectCache[sessionToolSelectionEntry](
			logger.With(attr.SlogCacheNamespace("session_tool_selection")),
			cacheImpl,
			cache.SuffixNone,
		),
		consentToolInventoryCache: cache.NewTypedObjectCache[consentToolInventory](
			logger.With(attr.SlogCacheNamespace("consent_tool_inventory")),
			cacheImpl,
			cache.SuffixNone,
		),
		sessionClientInfo:  sessionclientinfo.NewStore(redisClient, 0),
		identityResolver:   identityResolver,
		userSessionSigner:  userSessionSigner,
		remoteChallengeMgr: remoteChallengeMgr,
		remoteProxyManager: remoteProxyManager,
		tunnelManager:      newTunnelManager(tunnelRoutes, tunnelForwardToken, remoteProxyManager, tunnelGatewayCIDRs),
		tunnelPublic:       newTunnelPublicRuntime(redisClient, tunnelPublicConfig),
		metaRuntime:        metaRuntimeConfig.withDefaults(),
	}, nil
}

func (s *Service) requestAccessURL(ctx context.Context, serverID string, serverName string) string {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return ""
	}

	return mcpaccess.RequestAccessURL(s.siteURL, authCtx.OrganizationSlug, mcpaccess.RequestAccessURLParams{
		Scope:        "mcp:connect",
		ResourceID:   serverID,
		ResourceName: serverName,
	})
}

// PublicServerRoute is the chi route pattern for the public MCP endpoint: the
// JSON-RPC surface of a hosted MCP server. All three Streamable HTTP verbs
// mount here — POST for JSON-RPC messages, GET for the standalone SSE stream,
// and DELETE for session termination.
//
// The OAuth, metadata, and install routes extend this pattern with a further
// path segment. They are not the MCP endpoint itself, which is why anything
// keyed on "is this an MCP endpoint" has to match the pattern exactly rather
// than as a prefix.
const PublicServerRoute = "/mcp/{mcpSlug}"

func Attach(mux goahttp.Muxer, service *Service, metadataService *mcpmetadata.Service) {
	o11y.AttachHandler(mux, "POST", PlatformToolsetRoute, oops.ErrHandle(service.logger, service.ServePlatformToolset).ServeHTTP)
	o11y.AttachHandler(mux, "GET", "/mcp/idp_callback", oops.ErrHandle(service.logger, service.HandleIDPCallback).ServeHTTP)
	o11y.AttachHandler(mux, "GET", "/mcp/remote_login_callback", oops.ErrHandle(service.logger, service.HandleRemoteLoginCallback).ServeHTTP)
	// Backwards-compat: remote_session_clients flagged LegacyCallbackUrl were
	// registered upstream against the retired oauth_proxy_servers /oauth/callback.
	// Keep it mounted so their responses forward into remote_login_callback.
	o11y.AttachHandler(mux, "GET", "/oauth/callback", oops.ErrHandle(service.logger, service.HandleLegacyProxyCallback).ServeHTTP)
	// Public, unauthenticated outbound-CIMD document endpoint. Deployment-global
	// (not slug-scoped): clients are addressed by their globally unique id.
	o11y.AttachHandler(mux, "GET", "/.well-known/oauth-client/{id}", oops.ErrHandle(service.logger, service.HandleClientMetadataDocument).ServeHTTP)
	o11y.AttachHandler(mux, "GET", "/.well-known/openai-apps-challenge", oops.ErrHandle(service.logger, service.HandleOpenAIAppsChallenge).ServeHTTP)
	o11y.AttachHandler(mux, "POST", PublicServerRoute, oops.MCPErrHandle(service.logger, service.ServePublic).ServeHTTP)
	o11y.AttachHandler(mux, "GET", PublicServerRoute, oops.MCPErrHandle(service.logger, func(w http.ResponseWriter, r *http.Request) error {
		return service.HandleGetServer(w, r, metadataService)
	}).ServeHTTP)
	o11y.AttachHandler(mux, "DELETE", PublicServerRoute, oops.MCPErrHandle(service.logger, service.HandleDeleteServer).ServeHTTP)
	o11y.AttachHandler(mux, "GET", PublicServerRoute+"/install", oops.ErrHandle(service.logger, metadataService.ServeInstallPage).ServeHTTP)
	o11y.AttachHandler(mux, "GET", "/mcp/install-page-{hash}.js", oops.ErrHandle(service.logger, metadataService.ServeInstallPageScript).ServeHTTP)

	// OAuth metadata at the canonical RFC paths. The handlers in
	// authnchallenge.go dispatch internally on toolsets.user_session_issuer_id:
	// issuer-gated toolsets get the new metadata shape; legacy toolsets fall
	// through to wellknown.Resolve* (preserving the prior behaviour).
	o11y.AttachHandler(mux, "GET", wellknown.OAuthProtectedResourcePath+PublicServerRoute, oops.ErrHandle(service.logger, service.HandleGetProtectedResource).ServeHTTP)
	o11y.AttachHandler(mux, "GET", wellknown.OAuthAuthorizationServerPath+PublicServerRoute, oops.ErrHandle(service.logger, service.HandleGetAuthorizationServer).ServeHTTP)
	o11y.AttachHandler(mux, "POST", PublicServerRoute+"/register", oops.ErrHandle(service.logger, service.HandleRegister).ServeHTTP)
	o11y.AttachHandler(mux, "GET", PublicServerRoute+"/authorize", oops.ErrHandle(service.logger, service.HandleAuthorize).ServeHTTP)
	o11y.AttachHandler(mux, "GET", PublicServerRoute+"/idp_callback", oops.ErrHandle(service.logger, service.HandleIDPCallback).ServeHTTP)
	o11y.AttachHandler(mux, "GET", PublicServerRoute+"/connect", oops.ErrHandle(service.logger, service.HandleConsent).ServeHTTP)
	o11y.AttachHandler(mux, "POST", PublicServerRoute+"/connect", oops.ErrHandle(service.logger, service.HandleConsent).ServeHTTP)
	o11y.AttachHandler(mux, "POST", PublicServerRoute+"/connect/remote-session", oops.ErrHandle(service.logger, service.HandleConsentAction).ServeHTTP)
	o11y.AttachHandler(mux, "POST", PublicServerRoute+"/connect/mcp", oops.ErrHandle(service.logger, service.HandleConsentMCP).ServeHTTP)
	o11y.AttachHandler(mux, "DELETE", PublicServerRoute+"/connect/mcp", oops.ErrHandle(service.logger, service.HandleConsentMCP).ServeHTTP)
	o11y.AttachHandler(mux, "GET", PublicServerRoute+"/connect/first-party", oops.ErrHandle(service.logger, service.HandleFirstPartyConnect).ServeHTTP)
	o11y.AttachHandler(mux, "GET", "/mcp/consent-page-{hash}.js", oops.ErrHandle(service.logger, service.ServeConsentScript).ServeHTTP)
	o11y.AttachHandler(mux, "GET", "/mcp/consent-tools-{hash}.js", oops.ErrHandle(service.logger, service.ServeConsentToolsScript).ServeHTTP)
	o11y.AttachHandler(mux, "POST", PublicServerRoute+"/token", oops.ErrHandle(service.logger, service.HandleToken).ServeHTTP)
	o11y.AttachHandler(mux, "POST", PublicServerRoute+"/revoke", oops.ErrHandle(service.logger, service.HandleRevoke).ServeHTTP)
	o11y.AttachHandler(mux, "GET", PublicServerRoute+"/remote_login_callback", oops.ErrHandle(service.logger, service.HandleRemoteLoginCallback).ServeHTTP)
}

// HandleRemoteLoginCallback is the chi handler at
// `GET /mcp/remote_login_callback` (plus the legacy per-slug variant). Thin
// passthrough to remotesessions.ChallengeManager so /x/mcp can reuse the
// same handler via the public method instead of reaching into the
// unexported manager field.
func (s *Service) HandleRemoteLoginCallback(w http.ResponseWriter, r *http.Request) error {
	return s.remoteChallengeMgr.HandleRemoteLoginCallback(w, r) //nolint:wrapcheck // thin passthrough; the inner handler already writes the HTTP response.
}

// HandleLegacyProxyCallback is the chi handler at `GET /oauth/callback`. Thin
// passthrough to remotesessions.ChallengeManager: it forwards legacy
// oauth_proxy_servers-era callbacks (remote_session_clients flagged
// LegacyCallbackUrl) into /mcp/remote_login_callback.
func (s *Service) HandleLegacyProxyCallback(w http.ResponseWriter, r *http.Request) error {
	return s.remoteChallengeMgr.HandleLegacyProxyCallback(w, r) //nolint:wrapcheck // thin passthrough; the inner handler already writes the HTTP response.
}

// HandleClientMetadataDocument is the public outbound-CIMD document endpoint at
// `GET /.well-known/oauth-client/{id}`. Thin passthrough to
// remotesessions.ChallengeManager so the route mounts alongside the other
// remote-session handlers without reaching into the unexported manager field.
func (s *Service) HandleClientMetadataDocument(w http.ResponseWriter, r *http.Request) error {
	return s.remoteChallengeMgr.HandleClientMetadataDocument(w, r) //nolint:wrapcheck // thin passthrough; the inner handler already writes the HTTP response.
}

// HandleOpenAIAppsChallenge serves the domain-verification token configured
// for the custom domain resolved by middleware. The platform host has no
// custom-domain context and intentionally returns not found.
func (s *Service) HandleOpenAIAppsChallenge(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	domainCtx := customdomains.FromContext(ctx)
	if domainCtx == nil {
		return oops.E(oops.CodeNotFound, nil, "OpenAI apps challenge token not found").LogInfo(ctx, s.logger)
	}

	domain, err := customdomains_repo.New(s.db).GetCustomDomainByID(ctx, domainCtx.DomainID)
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeNotFound, err, "OpenAI apps challenge token not found").LogInfo(ctx, s.logger)
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "load OpenAI apps challenge token").LogError(ctx, s.logger)
	}
	if !domain.OpenaiAppsChallengeToken.Valid {
		return oops.E(oops.CodeNotFound, nil, "OpenAI apps challenge token not found").LogInfo(ctx, s.logger)
	}

	w.Header().Set("Content-Type", "text/plain")
	if _, err := w.Write([]byte(domain.OpenaiAppsChallengeToken.String)); err != nil {
		return fmt.Errorf("write OpenAI apps challenge token: %w", err)
	}
	return nil
}

// HandleGetServer handles GET requests to /mcp/{mcpSlug}. Browser requests
// (HTML Accept header) get the install page. SSE requests (Accept:
// text/event-stream) against a proxy-backed (remote/tunneled) mcp_server are
// the Streamable HTTP standalone server->client stream (spec § Listening for
// Messages from the Server) and dispatch through the unified endpoint
// dispatcher so the proxy relays them upstream. Everything else — including
// SSE requests against toolset-backed servers, which never send
// server-initiated messages — keeps the legacy 405.
func (s *Service) HandleGetServer(w http.ResponseWriter, r *http.Request, metadataService *mcpmetadata.Service) error {
	var wantsHTML, wantsSSE bool
	for mediaTypeFull := range strings.SplitSeq(r.Header.Get("Accept"), ",") {
		mediatype, params, err := mime.ParseMediaType(mediaTypeFull)
		if err != nil {
			continue
		}
		// An explicit q=0 marks the media type as not acceptable (RFC 9110
		// § 12.4.2) — never route toward a representation the client rejected.
		if q, qErr := strconv.ParseFloat(params["q"], 64); qErr == nil && q == 0 {
			continue
		}
		switch mediatype {
		case "text/html", "application/xhtml+xml":
			wantsHTML = true
		case "text/event-stream":
			wantsSSE = true
		}
	}

	if wantsHTML {
		// Intentionally NOT gated by enforceCustomDomainLockdown: the
		// install page must remain reachable on the platform host even
		// when the org's custom domain has an IP allowlist (private MCP
		// install pages rely on the platform-host session cookie). Only
		// the runtime paths (ServePublic, serveProxyBackedEndpoint) are
		// locked down.
		if err := metadataService.ServeInstallPage(w, r); err != nil {
			return fmt.Errorf("failed to serve install page: %w", err)
		}
		return nil
	}

	// The Streamable HTTP spec requires clients to send Accept:
	// text/event-stream on this GET; gating on it keeps health checkers and
	// other stray probes answering locally instead of generating upstream
	// noise.
	if wantsSSE {
		if handled, err := s.serveProxyBackedEndpoint(w, r); handled {
			return err
		}
	}

	return oops.E(oops.CodeMethodNotAllowed, nil, "This MCP server uses POST-based Streamable HTTP transport. This GET request is a normal compatibility probe by the MCP client and can be safely ignored. The client will automatically use POST for actual communication.")
}

// HandleDeleteServer handles DELETE requests to /mcp/{mcpSlug} — Streamable
// HTTP session termination (spec § Session Management). Proxy-backed servers
// relay it upstream so the remote session is actually torn down;
// toolset-backed servers hold no upstream session state, so the method stays
// unsupported there.
func (s *Service) HandleDeleteServer(w http.ResponseWriter, r *http.Request) error {
	if handled, err := s.serveProxyBackedEndpoint(w, r); handled {
		return err
	}
	return oops.E(oops.CodeMethodNotAllowed, nil, "session termination is not supported for this MCP server")
}

// serveProxyBackedEndpoint resolves {mcpSlug} and, when it maps to a
// proxy-backed (remote or tunneled) mcp_server, dispatches the request
// through the unified endpoint dispatcher — the same issuer gate + backend
// switch the POST path uses. handled=true means an authoritative outcome was
// reached (dispatched, or failed in a way the caller must propagate);
// handled=false means the slug does not resolve or resolves to a non-proxy
// backend, so callers fall back to their legacy behavior.
func (s *Service) serveProxyBackedEndpoint(w http.ResponseWriter, r *http.Request) (handled bool, err error) {
	ctx := r.Context()

	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return false, nil
	}
	logger := s.logger.With(attr.SlogToolsetMCPSlug(mcpSlug))

	mcpEndpoint, mcpServer, metaServer, err := s.ResolveMCPEndpointAndServer(ctx, logger, mcpSlug)
	var shareErr *oops.ShareableError
	switch {
	case err == nil:
	case errors.As(err, &shareErr) && shareErr.Code == oops.CodeNotFound:
		return false, nil
	default:
		return true, err
	}

	// Meta-backed endpoints hold no upstream session and no proxied GET/SSE
	// stream; the caller's legacy behavior (install page or 405) applies.
	if metaServer != nil {
		return false, nil
	}

	if !mcpServer.RemoteMcpServerID.Valid && !mcpServer.TunneledMcpServerID.Valid {
		return false, nil
	}

	if err := s.enforceCustomDomainLockdown(ctx, logger, mcpEndpoint.ProjectID); err != nil {
		return true, err
	}

	return true, s.serveResolvedMCPEndpoint(w, r, logger, mcpEndpoint, mcpServer, mcpSlug, "mcp")
}

// writeOAuthServerMetadataResponse builds the OAuth server metadata body and
// only commits the 200 OK status once the body is ready. This ordering matters:
// if marshaling fails or the result kind is unrecognized, the caller's error
// handler middleware needs an unwritten ResponseWriter so it can emit the real
// error status — Go's net/http silently drops a second WriteHeader call.
func writeOAuthServerMetadataResponse(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, r *http.Request, result *wellknown.OAuthServerMetadataResult) error {
	var body []byte
	switch result.Kind {
	case wellknown.OAuthServerMetadataResultKindRaw:
		body = result.Raw
	case wellknown.OAuthServerMetadataResultKindStatic:
		var marshalErr error
		body, marshalErr = json.Marshal(result.Static)
		if marshalErr != nil {
			return oops.E(oops.CodeUnexpected, marshalErr, "failed to marshal OAuth server metadata").LogError(ctx, logger)
		}
	default:
		return oops.E(oops.CodeUnexpected, nil, "unexpected OAuth server metadata result kind").LogError(ctx, logger)
	}

	return httpcache.WriteCacheableJSON(ctx, w, r, logger, "application/json", metadataCacheMaxAgeSeconds, body)
}

// writeOAuthProtectedResourceMetadataResponse builds the OAuth protected
// resource metadata body and only commits the 200 OK status once the body is
// ready. See writeOAuthServerMetadataResponse for the rationale behind the
// ordering.
func writeOAuthProtectedResourceMetadataResponse(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, r *http.Request, metadata *wellknown.OAuthProtectedResourceMetadata) error {
	body, err := json.Marshal(metadata)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to marshal OAuth protected resource metadata").LogError(ctx, logger)
	}

	return httpcache.WriteCacheableJSON(ctx, w, r, logger, "application/json", metadataCacheMaxAgeSeconds, body)
}

// ServePublic serves /mcp/{mcpSlug}. Resolution tries mcp_endpoints
// first — a slug bound to a custom-domain request resolves only against
// that domain; a slug arriving on the platform domain resolves only
// against (custom_domain_id IS NULL) endpoints. On a hit, dispatch
// matches /x/mcp: issuer-gated mcp_servers run the JWT gate before
// backend dispatch, then RemoteMcpServerID-backed rows proxy via
// remotemcp and ToolsetID-backed rows delegate to ServeToolsetResolved.
//
// On any not-found from endpoint resolution — no matching mcp_endpoint,
// dangling mcp_endpoint.mcp_server_id FK, or an mcp_server with
// visibility="disabled" — ServePublic falls back to the legacy
// toolsets.mcp_slug lookup. The fallback's loadToolset has
// platform/custom-domain handling distinct from mcp_endpoints'
// scoping: a platform-context lookup may resolve a toolset bound to a
// custom domain when no platform-scoped row exists. This asymmetry is
// load-bearing for customers that attached a custom domain to a
// pre-existing toolset without retiring the platform URL — see
// loadToolset's docstring and TestServePublic_CustomDomain_PlatformDomainStillWorks.
func (s *Service) ServePublic(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return r.Body.Close()
	})

	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided")
	}

	logger := s.logger.With(attr.SlogToolsetMCPSlug(mcpSlug))

	// Try mcp_endpoints → mcp_servers first. On hit, dispatch through the
	// unified backend switch (remote proxy / toolset). On 404, fall through
	// to the legacy toolset-by-slug path below.
	mcpEndpoint, mcpServer, metaServer, err := s.ResolveMCPEndpointAndServer(ctx, logger, mcpSlug)
	var shareErr *oops.ShareableError
	switch {
	case err == nil:
		if err := s.enforceCustomDomainLockdown(ctx, logger, mcpEndpoint.ProjectID); err != nil {
			return err
		}
		if metaServer != nil {
			return s.serveResolvedMetaMCPEndpoint(w, r, logger, mcpEndpoint, metaServer)
		}
		return s.serveResolvedMCPEndpoint(w, r, logger, mcpEndpoint, mcpServer, mcpSlug, "mcp")
	case errors.As(err, &shareErr) && shareErr.Code == oops.CodeNotFound:
		// Fall through to legacy toolset lookup.
	default:
		return err
	}

	var customDomainID uuid.NullUUID
	if domainCtx := customdomains.FromContext(ctx); domainCtx != nil {
		customDomainID = uuid.NullUUID{UUID: domainCtx.DomainID, Valid: true}
	}
	toolset, err := s.loadToolset(ctx, mcpSlug, customDomainID, false)
	switch {
	case errors.Is(err, errToolsetNotFound):
		return oops.E(oops.CodeNotFound, err, "mcp server not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "failed to load MCP server").LogError(ctx, s.logger)
	}

	if err := s.enforceCustomDomainLockdown(ctx, logger, toolset.ProjectID); err != nil {
		return err
	}

	// Legacy toolset-by-slug path has no mcp_server, so there is no
	// server-level variation group override (ServeToolsetResolved falls back to
	// the toolset's own column) and no fronting mcp_servers id to record.
	return s.ServeToolsetResolved(w, r, toolset, mcpSlug, "mcp", false, nil, nil, nil, nil)
}

// ServeToolsetResolved serves an MCP runtime request after the slug has
// already been resolved to a toolset. It is exported so other runtime
// surfaces (currently /x/mcp) can delegate the toolset-backed serving body
// without re-implementing the OAuth/visibility/RBAC and tool dispatch flow.
//
// mcpSlug and mcpRouteBase are used to build the WWW-Authenticate
// resource_metadata URL. mcpRouteBase is the route segment that sits
// between the well-known prefix and the slug — "mcp" for /mcp/{slug} or
// "x/mcp" for /x/mcp/{slug}, no leading or trailing slashes.
//
// skipIssuerGate skips the in-toolset user_session_issuer_id JWT-validation
// branch. /x/mcp callers set this to true once they have run their own
// gate keyed on mcp_servers.user_session_issuer_id, so the same request
// isn't gated twice. /mcp callers always pass false.
//
// extraUpstreamTokens are the upstream remote-session access tokens
// collected by a caller-side issuer gate (today: /x/mcp's pre-dispatch
// ApplyIssuerGate run), keyed by remote_session_issuer_id. When non-empty
// they satisfy the toolset's oauth2 security schemes so the downstream tool
// dispatch doesn't 401 when the in-toolset gate is skipped. /mcp callers
// pass nil.
//
// mcpServerVariationsGroupID is the variation group resolved from the
// mcp_servers row, when this request arrived via an mcp_endpoint that maps to
// one. It takes precedence over the toolset's own tool_variations_group_id;
// when nil, the toolset's column is used, and when that is also unset the
// project-default group applies. /mcp's legacy toolset-by-slug path has no
// mcp_server and passes nil.
//
// mcpServerID is the fronting mcp_servers row id when this request arrived via
// an mcp_endpoint, recorded on the tools/call telemetry row so toolset-backed
// activity can be sliced from the fronting-server perspective. /mcp's legacy
// toolset-by-slug path has no mcp_server and passes nil, leaving the attribute
// off the row.
//
// The caller is responsible for closing r.Body.
// callerToolSelection is the consent-screen tool policy resolved by a
// caller-side issuer gate (today: /x/mcp's pre-dispatch ApplyIssuerGate run).
// Nil when the caller ran no gate or the session carries no policy; the
// in-toolset gate below populates it for /mcp callers.
func (s *Service) ServeToolsetResolved(w http.ResponseWriter, r *http.Request, toolset *toolsets_repo.Toolset, mcpSlug, mcpRouteBase string, skipIssuerGate bool, extraUpstreamTokens map[uuid.UUID]remotesessions.UpstreamToken, callerToolSelection *toolfilter.SessionSelection, mcpServerVariationsGroupID *uuid.UUID, mcpServerID *uuid.UUID) error {
	return s.serveToolsetResolved(w, r, toolset, mcpSlug, mcpRouteBase, skipIssuerGate, extraUpstreamTokens, callerToolSelection, mcpServerVariationsGroupID, mcpServerID, nil)
}

func (s *Service) serveToolsetResolved(w http.ResponseWriter, r *http.Request, toolset *toolsets_repo.Toolset, mcpSlug, mcpRouteBase string, skipIssuerGate bool, extraUpstreamTokens map[uuid.UUID]remotesessions.UpstreamToken, callerToolSelection *toolfilter.SessionSelection, mcpServerVariationsGroupID *uuid.UUID, mcpServerID *uuid.UUID, pendingIssuerGate *issuerGateAuthentication) error {
	ctx := r.Context()
	var err error

	baseURL := s.serverURL.String()
	if customDomainCtx := customdomains.FromContext(ctx); customDomainCtx != nil {
		baseURL = fmt.Sprintf("https://%s", customDomainCtx.Domain)
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	bodyBytes, bodyReadErr := io.ReadAll(r.Body)
	var req rawRequest
	var bodyDecodeErr error
	if bodyReadErr == nil {
		bodyDecodeErr = json.Unmarshal(bodyBytes, &req)
		if bodyDecodeErr == nil {
			if rpcCtx, ok := contextvalues.GetRPCContext(ctx); ok && req.ID.IsSet() {
				rpcCtx.ID = req.ID
			}
		}
	}

	// Extract tokens from headers separately:
	// - authToken: from Authorization header (for OAuth flows)
	// - sessionToken: from Gram-Chat-Session header (for chat session fallback on non-OAuth endpoints)
	authToken := httpheaders.AuthorizationBearerToken(r)

	var tokenInputs []oauthTokenInputs

	// Token extraction — best effort for public MCPs with OAuth.
	// We collect tokens if present but don't return 401 here.
	// checkToolsetSecurity below enforces auth requirements and returns
	// false when unsatisfied, which triggers the 401 + WWW-Authenticate response.
	//
	// Private MCPs still enforce identity auth at this level since that's user
	// identity, not per-tool security.
	oauthRequired := toolset.ExternalOauthServerID.Valid

	// Issuer-gated path is fully separate from the legacy switch below: try
	// validating a user-session JWT; on success stamp ctx and skip the legacy
	// auth chain entirely; on miss, 401 with WWW-Authenticate so the client
	// can discover the AS surface.
	//
	// runInToolsetGate is the in-function variant of the gate, only run when
	// the toolset itself is issuer-gated AND the caller hasn't already gated
	// (skipIssuerGate). callerAlreadyGated tracks the orthogonal case where
	// the caller (/x/mcp) ran its own gate keyed on a different column
	// (mcp_servers.user_session_issuer_id) — the request has been
	// authenticated, so the legacy auth chain below must also be skipped or
	// it would re-reject the JWT it doesn't recognise.
	runInToolsetGate := toolset.UserSessionIssuerID.Valid && !skipIssuerGate
	callerAlreadyGated := skipIssuerGate
	if runInToolsetGate {
		// Pass mcpRouteBase (the surface the request arrived under) rather
		// than letting the constructor default to "mcp": when called from
		// /x/mcp the WWW-Authenticate URL, issuer URL, and consent action
		// all need to match the caller's surface, not the toolset's
		// canonical /mcp surface.
		endpoint := newResolvedMcpEndpointFromToolset(toolset, mcpRouteBase)
		newCtx, authentication, gateToolSelection, err := s.authenticateIssuerGate(ctx, w, authToken, baseURL, endpoint)
		if err != nil {
			return err
		}
		ctx = newCtx
		pendingIssuerGate = authentication
		callerToolSelection = gateToolSelection
	}

	isHostedToolsCall := bodyReadErr == nil && bodyDecodeErr == nil && req.Method == "tools/call" && mcpServerID != nil
	resolvePendingIssuerGate := func() error {
		if pendingIssuerGate == nil {
			return nil
		}

		gateTokens, err := s.resolveIssuerGateAccessTokens(ctx, w, pendingIssuerGate)
		if err != nil {
			return err
		}
		tokenInputs, err = appendRemoteSessionTokenInputs(tokenInputs, gateTokens)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "resolve upstream tokens for issuer-gated toolset").LogError(ctx, s.logger)
		}
		pendingIssuerGate = nil
		return nil
	}
	if pendingIssuerGate != nil && !isHostedToolsCall {
		if err := resolvePendingIssuerGate(); err != nil {
			return err
		}
	}

	oauthProtectedResourceURL, err := url.JoinPath(baseURL, wellknown.OAuthProtectedResourcePath, mcpRouteBase, mcpSlug)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to build OAuth protected resource URL").LogError(ctx, s.logger)
	}

	if !runInToolsetGate && !callerAlreadyGated {
		switch {
		case toolset.McpIsPublic && toolset.ExternalOauthServerID.Valid:
			// External OAuth server flow — collect token if present
			if authToken != "" {
				tokenInputs = append(tokenInputs, oauthTokenInputs{
					securityKeys:          []string{},
					remoteSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
					Token:                 authToken,
				})
			}
		case !toolset.McpIsPublic:
			ctx, err = s.RequirePrivateIdentityAuth(ctx, w, r, false, toolset.ID, oauthProtectedResourceURL)
			if err != nil {
				return err
			}
		default:
			ctx, err = s.TryPublicIdentityAuth(ctx, r, false, toolset.ID)
			if err != nil {
				return err
			}
		}
	}

	var selectedEnvironment string
	var authenticated bool
	if authCtx, ok := contextvalues.GetAuthContext(ctx); ok && authCtx != nil && authCtx.ActiveOrganizationID != "" {
		projects, err := s.authRepo.ListProjectsByOrganization(ctx, authCtx.ActiveOrganizationID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return oops.E(oops.CodeForbidden, nil, "no projects found").LogError(ctx, s.logger)
		case err != nil:
			return oops.E(oops.CodeUnexpected, err, "error checking project access").LogError(ctx, s.logger, attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
		}

		projectInOrg := false
		for _, project := range projects {
			if project.ID == toolset.ProjectID {
				projectInOrg = true
				break
			}
		}

		if projectInOrg {
			authenticated = true
		} else if !toolset.McpIsPublic {
			// Only return 401 for non-public MCPs when the user is not in the owning org
			return oops.C(oops.CodeUnauthorized)
		}
		// For public MCPs accessed from outside the owning org, authenticated stays false
		// so they get public access without environment/secrets
	}

	if !toolset.McpIsPublic && !authenticated {
		return oops.C(oops.CodeNotFound)
	}

	if authenticated {
		// Private MCPs require mcp:connect on the specific toolset.
		// Public MCPs are open to everyone — no RBAC enforcement.
		if !toolset.McpIsPublic {
			// Ensure grants are loaded — not all auth strategies in authenticateToken
			// go through auth.Authorize (which calls PrepareContext). This is a no-op
			// if grants are already in context.
			ctx, err = s.authz.PrepareContext(ctx)
			if err != nil {
				return oops.E(oops.CodeUnexpected, err, "failed to load access grants").LogError(ctx, s.logger)
			}
			if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPConnect, toolset.ID.String(), toolset.ProjectID.String())); err != nil {
				return fmt.Errorf("authorize MCP server access: %w", mcpaccess.ServerPermissionDenied(err, s.requestAccessURL(ctx, toolset.ID.String(), toolset.Name)))
			}
		}

	}

	// Decode the raw body first to check for batch requests
	switch {
	case errors.Is(bodyReadErr, io.EOF) || len(bodyBytes) == 0:
		return nil
	case bodyReadErr != nil:
		return oops.E(oops.CodeBadRequest, bodyReadErr, "failed to read request body").LogError(ctx, s.logger)
	}

	// Reject batch (array) requests — batch is deprecated in the MCP spec
	if err := inv.Check("mcp request",
		"not a batch request", len(bodyBytes) == 0 || bodyBytes[0] != '[',
	); err != nil {
		return oops.E(oops.CodeBadRequest, err, "batch requests are not supported").LogError(ctx, s.logger)
	}

	if bodyDecodeErr != nil {
		return oops.E(oops.CodeBadRequest, bodyDecodeErr, "failed to decode request body").LogError(ctx, s.logger)
	}
	hostedCoverageRecorded := false
	if isHostedToolsCall {
		if err := s.enforceHostedToolsCall(ctx, toolset.OrganizationID, mcpServerID); err != nil {
			return s.respondMCPError(ctx, w, req.ID, err)
		}
		hostedCoverageRecorded = true
	}

	// Resolve tool configuration and credentials only after the hosted checkpoint.
	toolVariationsGroupID := mcpServerVariationsGroupID
	if toolVariationsGroupID == nil && toolset.ToolVariationsGroupID.Valid {
		id := toolset.ToolVariationsGroupID.UUID
		toolVariationsGroupID = &id
	}
	tags := parseTagsFilter(r.URL.Query().Get("tags"))
	if authenticated {
		selectedEnvironment = conv.PtrValOr(conv.FromPGText[string](toolset.DefaultEnvironmentSlug), "")
		if passedEnv := r.Header.Get("Gram-Environment"); passedEnv != "" {
			selectedEnvironment = conv.ToSlug(passedEnv)
		}
	}
	tokenInputs, err = appendRemoteSessionTokenInputs(tokenInputs, extraUpstreamTokens)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "resolve upstream tokens for issuer-gated toolset").LogError(ctx, s.logger)
	}

	if err := resolvePendingIssuerGate(); err != nil {
		return err
	}

	sessionID := parseMcpSessionID(r.Header)
	if req.Method == "initialize" {
		w.Header().Set("Mcp-Session-Id", sessionID)
	}

	// Load header display names for remapping
	headerDisplayNames := s.loadHeaderDisplayNames(ctx, toolset.ID)

	// Extract user IDs for telemetry
	var userID, externalUserID, apiKeyID string
	if authCtx, ok := contextvalues.GetAuthContext(ctx); ok && authCtx != nil {
		userID = authCtx.UserID
		externalUserID = authCtx.ExternalUserID
		apiKeyID = authCtx.APIKeyID
	}

	mcpInputs := &mcpInputs{
		projectID:                toolset.ProjectID,
		organizationID:           toolset.OrganizationID,
		toolset:                  toolset.Slug,
		environment:              selectedEnvironment,
		mcpEnvVariables:          parseMcpEnvVariables(r, headerDisplayNames),
		authenticated:            authenticated,
		oauthTokenInputs:         tokenInputs,
		sessionID:                sessionID,
		chatID:                   r.Header.Get("Gram-Chat-ID"),
		mode:                     resolveToolMode(r, *toolset),
		userID:                   userID,
		externalUserID:           externalUserID,
		apiKeyID:                 apiKeyID,
		toolVariationsGroupID:    toolVariationsGroupID,
		mcpServerID:              mcpServerID,
		skipProxyTools:           false,
		tags:                     tags,
		protocolVersion:          mcpversions.Resolve(mcprequests.DeclaredProtocolVersion(r.Header.Get(mcpversions.HTTPHeader), req.Params), mcpversions.SupportedHostedToolset()),
		identityCoverageRecorded: hostedCoverageRecorded,
		toolSelection:            callerToolSelection,
	}

	// Record the resolved variation group and requested tag filter for
	// debugging which tools a client sees.
	if span := trace.SpanFromContext(ctx); span.IsRecording() {
		if toolVariationsGroupID != nil {
			span.SetAttributes(attr.ToolVariationsGroupID(toolVariationsGroupID.String()))
		}
		if len(tags) > 0 {
			span.SetAttributes(attr.MCPRequestedTags(tags))
		}
	}

	// Check security schemes before dispatching any RPC — including initialize.
	// Some MCP clients (e.g. Claude Desktop) require 401 on initialize to trigger
	// their OAuth flow, so we can't defer this to individual RPC handlers.
	satisfied, err := s.checkToolsetSecurity(ctx, toolset, mcpInputs)
	if err != nil {
		return err
	}
	if !satisfied {
		if oauthRequired {
			w.Header().Set(
				"WWW-Authenticate",
				fmt.Sprintf(`Bearer resource_metadata="%s"`, oauthProtectedResourceURL),
			)
		}
		return oops.C(oops.CodeUnauthorized)
	}

	body, err := s.handleRequest(ctx, mcpInputs, &req)
	switch {
	case body == nil && err == nil:
		return respondWithNoContent(true, w)
	case err != nil:
		mcpID := mcpjsonrpc.NullID()
		if rpcCtx, ok := contextvalues.GetRPCContext(ctx); ok && rpcCtx.ID.IsSet() {
			mcpID = rpcCtx.ID
		}
		return s.respondMCPError(ctx, w, mcpID, err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, writeErr := w.Write(body)
	if writeErr != nil {
		return oops.E(oops.CodeUnexpected, writeErr, "failed to write response body")
	}

	return nil
}

func (s *Service) enforceHostedToolsCall(ctx context.Context, organizationID string, mcpServerID *uuid.UUID) error {
	if mcpServerID == nil {
		return nil
	}

	serverSource := mcptoolexecution.ServerSource{
		FrontingServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	}
	if mcpServerID != nil {
		serverSource.FrontingServerID = uuid.NullUUID{UUID: *mcpServerID, Valid: true}
	}

	disposition, err := s.hostedToolsCallCheckpoint.Evaluate(ctx, organizationID, serverSource)
	if err != nil {
		return fmt.Errorf("evaluate hosted MCP kill switch: %w", err)
	}
	switch disposition.Kind() {
	case killswitches.TransportDispositionContinue:
		return nil
	case killswitches.TransportDispositionMatchedDenial:
		note, ok := disposition.ExternalNote()
		if !ok {
			return errors.New("matched MCP kill-switch disposition has no external note")
		}
		return &oops.MCPError{
			ID:      mcpjsonrpc.ID{Number: 0, String: ""},
			Code:    oops.MCPCodeForbidden,
			Message: note,
			Data:    &oops.MCPErrorData{Code: oops.MCPErrorDataCodeToolCallsPaused},
		}
	case killswitches.TransportDispositionInfrastructureRejection:
		return &oops.MCPError{ID: mcpjsonrpc.ID{Number: 0, String: ""}, Code: oops.MCPCodeInternalError, Message: "", Data: nil}
	default:
		return errors.New("invalid hosted MCP kill-switch disposition")
	}
}

func (s *Service) respondMCPError(ctx context.Context, w http.ResponseWriter, id mcpjsonrpc.ID, cause error) error {
	bs, err := json.Marshal(oops.NewMCPErrorFromCause(id, cause))
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to serialize error response").LogError(ctx, s.logger)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(bs); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to write MCP error response")
	}
	return nil
}

// checkToolsetSecurity loads the toolset's security variables and checks if the
// request environment satisfies at least one scheme. Returns true if satisfied
// (or if the toolset has no security requirements).
func (s *Service) checkToolsetSecurity(ctx context.Context, toolset *toolsets_repo.Toolset, payload *mcpInputs) (bool, error) {
	projectID := mv.ProjectID(payload.projectID)
	// Security-scheme detection must see the full, unfiltered toolset, so this
	// always uses the project-default variation group (nil) regardless of any
	// ?groups= filter on the request. Variations never change a tool's security
	// requirements, so this does not weaken the auth gate.
	described, err := mv.DescribeToolset(ctx, s.logger, s.db, projectID, mv.ToolsetSlug(conv.ToLower(payload.toolset)), &s.toolsetCache, nil, s.platformExtras...)
	if err != nil {
		return false, oops.E(oops.CodeUnexpected, err, "failed to describe toolset for security check").LogError(ctx, s.logger)
	}

	schemes := describeToolSecurity(described.SecurityVariables)
	if len(schemes) == 0 {
		// No per-tool security annotations, but the toolset may still require
		// OAuth at the server level (proxy or external). If so, require the
		// user to have provided a token — otherwise the 401 + WWW-Authenticate
		// must be sent so MCP clients can initiate the OAuth flow.
		oauthRequired := toolset.McpIsPublic && (toolset.ExternalOauthServerID.Valid || toolset.OauthProxyServerID.Valid)
		if oauthRequired {
			for _, t := range payload.oauthTokenInputs {
				if t.Token != "" {
					return true, nil
				}
			}
			return false, nil
		}
		return true, nil
	}

	systemEnv, err := s.env.LoadSystemEnv(ctx, payload.projectID, toolset.ID, "", "")
	if err != nil {
		return false, oops.E(oops.CodeUnexpected, err, "failed to load system environment").LogError(ctx, s.logger)
	}

	mergedEnv := toolconfig.NewCaseInsensitiveEnv()
	for k, v := range systemEnv.All() {
		mergedEnv.Set(k, v)
	}

	// Load authenticated user's Gram environment.
	if payload.environment != "" && payload.authenticated {
		storedEnvVars, err := s.env.Load(ctx, payload.projectID, toolconfig.Slug(payload.environment))
		if err != nil && !errors.Is(err, toolconfig.ErrNotFound) {
			s.logger.WarnContext(ctx, "failed to load user environment for security check", attr.SlogError(err))
		}
		for k, v := range storedEnvVars {
			mergedEnv.Set(k, v)
		}
	}

	// Merge MCP request headers.
	for k, v := range payload.mcpEnvVariables {
		mergedEnv.Set(k, v)
	}

	// Map any OAuth tokens to ACCESS_TOKEN env vars on OAuth schemes.
	var oauthToken string
	for _, t := range payload.oauthTokenInputs {
		if t.Token != "" {
			oauthToken = t.Token
			break
		}
	}
	if oauthToken != "" {
		for _, sv := range described.SecurityVariables {
			if sv.Type == nil {
				continue
			}
			if *sv.Type == "oauth2" || *sv.Type == "openIdConnect" {
				for _, envVar := range sv.EnvVariables {
					if strings.HasSuffix(envVar, "ACCESS_TOKEN") {
						mergedEnv.Set(envVar, oauthToken)
					}
				}
			}
		}
	}

	return anySchemeSatisfied(schemes, mergedEnv, oauthToken), nil
}

// loadToolset loads the toolset for an mcp_slug. The lookup dispatches
// on customDomainID and strictPlatform:
//
//   - customDomainID.Valid → scoped strictly to that custom domain.
//   - customDomainID zero + strictPlatform=true → platform-only (custom_domain_id IS NULL).
//   - customDomainID zero + strictPlatform=false → loose: prefers platform but
//     accepts a custom-domain row matching the slug if no platform row exists.
//
// Runtime callers (route-driven) derive customDomainID from
// customdomains.FromContext and pass strictPlatform=false so legacy
// slug-routed requests on the platform URL keep resolving custom-domain
// toolsets (load-bearing for TestServePublic_CustomDomain_PlatformDomainStillWorks
// — customers attach custom domains to existing toolsets without retiring
// the platform URL).
//
// Stored-state callers (resuming a cached EndpointRef) pass
// strictPlatform=true to assert that the original challenge was minted
// on the platform domain — the value is an explicit assertion rather
// than a route inference.
//
// Disabled toolsets (mcp_enabled false) surface as errToolsetNotFound so
// every legacy-routed surface (serving, well-known metadata, OAuth
// challenges) treats them as nonexistent — mirroring how the
// mcp_endpoints → mcp_servers path handles visibility 'disabled'.
func (s *Service) loadToolset(ctx context.Context, mcpSlug string, customDomainID uuid.NullUUID, strictPlatform bool) (*toolsets_repo.Toolset, error) {
	var toolset toolsets_repo.Toolset
	var err error
	switch {
	case customDomainID.Valid:
		toolset, err = s.toolsetsRepo.GetToolsetByMcpSlugAndCustomDomain(ctx, toolsets_repo.GetToolsetByMcpSlugAndCustomDomainParams{
			McpSlug:        conv.ToPGText(mcpSlug),
			CustomDomainID: customDomainID,
		})
	case strictPlatform:
		toolset, err = s.toolsetsRepo.GetToolsetByPlatformMcpSlug(ctx, conv.ToPGText(mcpSlug))
	default:
		toolset, err = s.toolsetsRepo.GetToolsetByMcpSlug(ctx, conv.ToPGText(mcpSlug))
	}
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, errToolsetNotFound
	case err != nil:
		return nil, fmt.Errorf("lookup toolset: %w", err)
	}
	if !toolset.McpEnabled {
		return nil, errToolsetNotFound
	}
	return &toolset, nil
}

// loadHeaderDisplayNames loads the header display names mapping from MCP metadata.
// Returns an empty map if no metadata exists or on error (non-critical operation).
func (s *Service) loadHeaderDisplayNames(ctx context.Context, toolsetID uuid.UUID) map[string]string {
	result := make(map[string]string)

	displayNamesJSON, err := s.mcpMetadataRepo.GetHeaderDisplayNames(ctx, uuid.NullUUID{UUID: toolsetID, Valid: true})
	if err != nil {
		// Not found or error - return empty map, this is non-critical
		return result
	}

	if len(displayNamesJSON) > 0 {
		if parseErr := json.Unmarshal(displayNamesJSON, &result); parseErr != nil {
			s.logger.WarnContext(ctx, "failed to parse header display names", attr.SlogError(parseErr))
		}
	}

	return result
}

// TODO: this is for demo. There probably needs to still be annotation per toolset on if it allows dynamic tool calling
// Realistically you would need to embed and vectorize ahead of time
func resolveToolMode(r *http.Request, toolset toolsets_repo.Toolset) ToolMode {
	mode := r.Header.Get("Gram-Mode")
	mode = strings.TrimSpace(mode)
	mode = strings.ToLower(mode)

	if mode != "" {
		return ToolMode(mode)
	} else if toolset.ToolSelectionMode != "" {
		return ToolMode(toolset.ToolSelectionMode)
	}

	return ToolModeStatic
}

// parseMcpEnvVariables: Map potential user provided mcp variables into inputs
// Only inputs that match up with a security or server env var in the proxy will be used in the proxy
// headerDisplayNames maps actual header names (e.g., "X-RapidAPI-Key") to display names (e.g., "API Key")
// When a display name is used in the MCP header, it's mapped back to the actual header's env var
func parseMcpEnvVariables(r *http.Request, headerDisplayNames map[string]string) map[string]string {
	ignoredHeaders := []string{
		"mcp-session-id",
	}

	// Build reverse mapping: normalized_display_name -> normalized_actual_name
	// This allows users to send MCP-API-KEY and have it mapped to X_RAPIDAPI_KEY
	displayNameToActual := make(map[string]string)
	for actualName, displayName := range headerDisplayNames {
		if displayName != "" {
			// Normalize: lowercase and replace dashes with underscores
			normalizedDisplayName := strings.ToLower(strings.ReplaceAll(displayName, "-", "_"))
			normalizedDisplayName = strings.ReplaceAll(normalizedDisplayName, " ", "_")
			normalizedActual := strings.ToLower(strings.ReplaceAll(actualName, "-", "_"))
			displayNameToActual[normalizedDisplayName] = normalizedActual
		}
	}

	envVars := map[string]string{}
	for k := range r.Header {
		keySanitized := strings.ToLower(k)
		if strings.HasPrefix(keySanitized, "mcp-") && !slices.Contains(ignoredHeaders, keySanitized) {
			// Extract the key without MCP- prefix and normalize
			normalizedKey := strings.ReplaceAll(strings.TrimPrefix(keySanitized, "mcp-"), "-", "_")

			// Check if this is a display name and map to actual header name
			actualKey, aliased := displayNameToActual[normalizedKey]
			if aliased {
				normalizedKey = actualKey
			}

			// The MCP-Protocol-Version header is protocol metadata every
			// conforming client stamps on every request since 2025-06-18, and
			// without this skip it silently becomes a `protocol_version`
			// variable. The skip is alias-aware: a toolset whose configured
			// display name maps to it keeps receiving it as before.
			//
			// The remaining 2026-07-28 standard headers (Mcp-Method, Mcp-Name,
			// Mcp-Param-*; httpheaders.IsStandardMCPRequestHeader is the
			// canonical set) are deliberately NOT skipped yet. Clients on that
			// revision are not measurably present, while skipping now would
			// silently break any variable whose actual name collides — default
			// variable headers are minted as MCP-<VAR> and never appear in the
			// display-name alias map, so the alias exception cannot save them.
			// Reserving those headers belongs to the 2026-07-28 support work,
			// where header-body validation gives clients a visible rejection
			// instead of a silently dropped value.
			if !aliased && strings.EqualFold(keySanitized, mcpversions.HTTPHeader) {
				continue
			}

			envVars[normalizedKey] = r.Header.Get(k)
		}

	}

	return envVars
}

func (s *Service) handleRequest(ctx context.Context, payload *mcpInputs, req *rawRequest) (json.RawMessage, error) {
	// The census dimension is what the request declared, not the in-effect
	// revision: clamping keeps absent and unknown declarations countable,
	// and the resolved value would fabricate a revision for clients that
	// named none. Handlers that consume the rest of the per-request metadata
	// decode it themselves (tools/call in the same pass as its params,
	// tools/list scoped to its analytics event).
	s.metrics.RecordMCPRequest(ctx, payload.protocolVersion.Declared, req.Method, mcpmetrics.SurfaceHosting)

	if requestContext, _ := contextvalues.GetRequestContext(ctx); requestContext != nil {
		start := time.Now()
		defer func() {
			s.metrics.RecordMCPRequestDuration(ctx, req.Method, requestContext.Host+requestContext.ReqURL, time.Since(start))
		}()
	}

	switch req.Method {
	case "ping":
		return handlePing(ctx, s.logger, req.ID, serverInfoHostedToolset)
	case "initialize":
		return handleInitialize(ctx, s.logger, s.metrics, req, payload, s.posthog, s.toolsetsRepo, s.mcpMetadataRepo, s.sessionClientInfo)
	case "notifications/initialized", "notifications/cancelled":
		return nil, nil
	case "tools/list":
		return handleToolsList(ctx, s.logger, s.authz, s.guardianPolicy, s.db, s.env, payload, req, s.posthog, &s.toolsetCache, s.vectorToolStore, s.temporal, s.shadowMCPClient, s.platformExtras, s.sessionClientInfo)
	case "tools/call":
		recordToolsCallIdentityCoverage(ctx, s.identityCoverage, payload.organizationID, payload)
		return handleToolsCall(ctx, s.logger, s.metrics, s.identityCoverage, s.authz, s.guardianPolicy, s.db, s.env, payload, req, s.toolProxy, s.billingTracker, s.billingRepository, &s.toolsetCache, s.telemLogger, s.vectorToolStore, s.temporal, s.mcpMetadataRepo, s.auditLogger, s.platformExtras, s.sessionClientInfo)
	case "prompts/list":
		return handlePromptsList(ctx, s.logger, s.db, payload, req, &s.toolsetCache, s.platformExtras)
	case "prompts/get":
		return handlePromptsGet(ctx, s.logger, s.db, payload, req)
	case "resources/list":
		return handleResourcesList(ctx, s.logger, s.db, payload, req, &s.toolsetCache, s.platformExtras)
	case "resources/templates/list":
		return handleResourcesTemplatesList(ctx, s.logger, req)
	case "resources/read":
		return handleResourcesRead(ctx, s.logger, s.db, payload, req, s.toolProxy, s.env, s.billingTracker, s.billingRepository, s.telemLogger, s.platformExtras)
	default:
		return nil, oops.E(oops.CodeNotImplemented, nil, "%s: %s", req.Method, oops.MCPCodeMethodNotFound.Message())
	}
}

func parseMcpSessionID(headers http.Header) string {
	session := headers.Get("Mcp-Session-Id")
	if session == "" {
		session = uuid.New().String()
	}
	return session
}

// parseTagsFilter parses the ?tags= query value into a deduplicated set of tag
// names. The value is comma-separated; surrounding whitespace and empty
// segments are dropped. An absent or empty value yields nil, meaning no
// filtering is applied.
func parseTagsFilter(raw string) []string {
	if raw == "" {
		return nil
	}

	seen := make(map[string]struct{})
	tags := make([]string, 0)
	for part := range strings.SplitSeq(raw, ",") {
		tag := strings.TrimSpace(part)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		tags = append(tags, tag)
	}

	if len(tags) == 0 {
		return nil
	}

	return tags
}

// RequirePrivateIdentityAuth runs identity authentication for a non-public
// MCP. It tries the Authorization header first, then the
// Gram-Chat-Session header, returning the authenticated context on success.
// On failure, when isOAuthCapable, it sets a WWW-Authenticate header with
// the supplied resource_metadata URL so MCP clients can initiate OAuth, and
// returns 401.
func (s *Service) RequirePrivateIdentityAuth(ctx context.Context, w http.ResponseWriter, r *http.Request, isOAuthCapable bool, oauthResourceID uuid.UUID, wwwAuthResourceMetadataURL string) (context.Context, error) {
	token := httpheaders.AuthorizationOrChatSessionToken(r)

	authedCtx, err := s.authenticateToken(ctx, token, oauthResourceID, isOAuthCapable)
	if err == nil {
		return authedCtx, nil
	}

	if isOAuthCapable {
		w.Header().Set(
			"WWW-Authenticate",
			fmt.Sprintf(`Bearer resource_metadata="%s"`, wwwAuthResourceMetadataURL),
		)
	}

	return ctx, oops.E(oops.CodeUnauthorized, nil, "expired or invalid access token")
}

// TryPublicIdentityAuth optionally authenticates a public MCP request when
// the caller supplies an Authorization or Gram-Chat-Session token. Missing
// tokens are not an error; an invalid supplied token is.
func (s *Service) TryPublicIdentityAuth(ctx context.Context, r *http.Request, isOAuthCapable bool, oauthResourceID uuid.UUID) (context.Context, error) {
	token := httpheaders.AuthorizationOrChatSessionToken(r)
	if token == "" {
		return ctx, nil
	}

	authedCtx, err := s.authenticateToken(ctx, token, oauthResourceID, isOAuthCapable)
	if err != nil {
		return ctx, err
	}

	if authCtx, ok := contextvalues.GetAuthContext(authedCtx); !ok || authCtx == nil {
		return ctx, oops.E(oops.CodeUnauthorized, nil, "no auth context found").LogError(ctx, s.logger)
	}
	return authedCtx, nil
}

// authenticateToken authenticates the caller using the supplied token across
// several strategies (assistant tokens, gram OAuth via oauthResourceID, API
// keys, chat sessions). oauthResourceID is consumed only when isOAuthCapable
// is true — today that path is exercised only by toolset-backed flows so
// the resource is a toolset id; remote-backend callers pass false and the
// id is decorative.
//
// Each successful strategy stamps its mcpidentity provenance here, at the
// point of credential validation: assistant tokens are KindAssistant, API
// keys (either scope) are KindAPIKey, and chat-session tokens are
// KindChatSession. None of these credentials proves an acting Gram user, so
// none stamps KindUserSession — even though every strategy populates an
// AuthContext whose user-shaped fields exist for attribution only. A token
// rejected by every strategy leaves the context unstamped, so downstream
// checkpoints classify the request as unattributed.
func (s *Service) authenticateToken(ctx context.Context, token string, oauthResourceID uuid.UUID, isOAuthCapable bool) (context.Context, error) {
	if token == "" {
		return ctx, oops.C(oops.CodeUnauthorized)
	}

	if authorizedCtx, _, err := s.assistantTokens.Authorize(ctx, token); err == nil {
		return mcpidentity.WithIdentity(authorizedCtx, mcpidentity.Identity{Kind: mcpidentity.KindAssistant, UserID: ""}), nil
	}

	var err error

	// Strategy 2: Try API key authentication (consumer scope)
	sc := security.APIKeyScheme{
		Name:           constants.KeySecurityScheme,
		RequiredScopes: []string{"consumer"},
		Scopes:         []string{},
	}

	ctx, err = s.auth.Authorize(ctx, token, &sc)
	if err == nil {
		return mcpidentity.WithIdentity(ctx, mcpidentity.Identity{Kind: mcpidentity.KindAPIKey, UserID: ""}), nil
	}

	// Strategy 3: Try API key authentication (chat scope fallback)
	sc = security.APIKeyScheme{
		Name:           constants.KeySecurityScheme,
		RequiredScopes: []string{"chat"},
		Scopes:         []string{},
	}
	ctx, err = s.auth.Authorize(ctx, token, &sc)
	if err == nil {
		return mcpidentity.WithIdentity(ctx, mcpidentity.Identity{Kind: mcpidentity.KindAPIKey, UserID: ""}), nil
	}

	// Strategy 4: Try Chat Sessions Token authentication
	ctx, err = s.chatSessionsManager.Authorize(ctx, token)
	if err == nil {
		return mcpidentity.WithIdentity(ctx, mcpidentity.Identity{Kind: mcpidentity.KindChatSession, UserID: ""}), nil
	}

	return ctx, oops.E(oops.CodeUnauthorized, errors.New("failed to authorize token using any strategy"), "failed to authorize").LogWarn(ctx, s.logger, attr.SlogToolsetID(oauthResourceID.String()))
}

// HandleToolsList executes tools/list RPC for internal clients (e.g., agent workflows).
// This method provides direct access to tool listing without HTTP overhead.
func (s *Service) HandleToolsList(
	ctx context.Context,
	inputs *McpInputs,
) (*ToolListResult, error) {
	// Convert exported inputs to internal format
	payload := inputs.toInternal()

	// Create a dummy rawRequest for the internal handler
	req := &rawRequest{
		JSONRPC: "2.0",
		ID:      mcpjsonrpc.NumberID(1),
		Method:  "tools/list",
		Params:  json.RawMessage("{}"),
	}

	// Call existing handleToolsList with all dependencies. Internal callers
	// carry no HTTP request and therefore no per-request `_meta`.
	result, err := handleToolsList(
		ctx,
		s.logger,
		s.authz,
		s.guardianPolicy,
		s.db,
		s.env,
		payload,
		req,
		s.posthog,
		&s.toolsetCache,
		s.vectorToolStore,
		s.temporal,
		s.shadowMCPClient,
		s.platformExtras,
		s.sessionClientInfo,
	)
	if err != nil {
		return nil, fmt.Errorf("handle tools list: %w", err)
	}

	// Parse the JSON result
	var internalResult toolsListResult
	if err := json.Unmarshal(result, &internalResult); err != nil {
		return nil, fmt.Errorf("unmarshal tools list result: %w", err)
	}

	// Convert internal result to exported format
	tools := make([]ToolListEntry, len(internalResult.Result.Tools))
	for i, t := range internalResult.Result.Tools {
		tools[i] = ToolListEntry{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
			Annotations: t.Annotations,
			Meta:        t.Meta,
		}
	}

	return &ToolListResult{
		Tools: tools,
	}, nil
}

// HandleToolsCall executes tools/call RPC for internal clients (e.g., agent workflows).
// This method provides direct access to tool execution without HTTP overhead.
func (s *Service) HandleToolsCall(
	ctx context.Context,
	inputs *McpInputs,
	toolName string,
	arguments json.RawMessage,
) (*ToolCallResult, error) {
	// Convert exported inputs to internal format
	payload := inputs.toInternal()

	// Construct rawRequest with tools/call parameters
	params, err := json.Marshal(map[string]any{
		"name":      toolName,
		"arguments": arguments,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal tool call params: %w", err)
	}

	req := &rawRequest{
		JSONRPC: "2.0",
		ID:      mcpjsonrpc.NumberID(1),
		Method:  "tools/call",
		Params:  params,
	}

	// Call existing handleToolsCall
	result, err := handleToolsCall(
		ctx,
		s.logger,
		s.metrics,
		s.identityCoverage,
		s.authz,
		s.guardianPolicy,
		s.db,
		s.env,
		payload,
		req,
		s.toolProxy,
		s.billingTracker,
		s.billingRepository,
		&s.toolsetCache,
		s.telemLogger,
		s.vectorToolStore,
		s.temporal,
		s.mcpMetadataRepo,
		s.auditLogger,
		s.platformExtras,
		s.sessionClientInfo,
	)
	if err != nil {
		return nil, fmt.Errorf("handle tool call: %w", err)
	}

	// Parse the JSON result wrapper
	var wrapper struct {
		Result struct {
			Content []json.RawMessage `json:"content"`
			IsError bool              `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(result, &wrapper); err != nil {
		return nil, fmt.Errorf("unmarshal tool call result: %w", err)
	}

	// Convert content chunks from json.RawMessage to ContentChunk
	content := make([]ContentChunk, len(wrapper.Result.Content))
	for i, rawChunk := range wrapper.Result.Content {
		var chunk struct {
			Type     string  `json:"type"`
			Text     string  `json:"text,omitempty"`
			Data     string  `json:"data,omitempty"`
			MimeType *string `json:"mimeType,omitempty"`
		}
		if err := json.Unmarshal(rawChunk, &chunk); err != nil {
			return nil, fmt.Errorf("unmarshal content chunk %d: %w", i, err)
		}

		mimeType := ""
		if chunk.MimeType != nil {
			mimeType = *chunk.MimeType
		}

		content[i] = ContentChunk{
			Type:     chunk.Type,
			Text:     chunk.Text,
			Data:     chunk.Data,
			MimeType: mimeType,
		}
	}

	return &ToolCallResult{
		Content: content,
		IsError: wrapper.Result.IsError,
	}, nil
}
