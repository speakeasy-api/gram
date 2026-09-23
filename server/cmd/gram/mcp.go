package gram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/sourcegraph/conc/pool"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.temporal.io/sdk/client"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/control"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata"
	mcpmetadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/memory"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/modelkeys"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	platformtoolsruntime "github.com/speakeasy-api/gram/server/internal/platformtools/runtime"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/rag"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	slack_client "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/speakeasy-api/gram/server/internal/xmcp"
)

// mcpServerFlags accepts the same runtime configuration as `gram start` so the
// MCP tier can run from the same environment as the API server. Temporal and
// worker flags are accepted but never read: this command has no Temporal
// dependency by design.
func mcpServerFlags() []cli.Flag {
	flags := mcpRuntimeFlags()
	return append(flags,
		&cli.StringFlag{Name: "address", Value: ":8080", Usage: "HTTP address to listen on", EnvVars: []string{"GRAM_SERVER_ADDRESS"}},
		&cli.StringFlag{Name: "ssl-key-file", Usage: "The SSL key file path to use for the server", EnvVars: []string{"GRAM_SSL_KEY_FILE"}},
		&cli.StringFlag{Name: "ssl-cert-file", Usage: "The SSL certificate file path to use for the server", EnvVars: []string{"GRAM_SSL_CERT_FILE"}},
	)
}

// newMCPCommand starts the public MCP and OAuth serving tier: the /mcp and
// /x/mcp servers, their OAuth authorization surface and well-known metadata,
// and the retired OAuth proxy tombstone. It deliberately duplicates the parts
// of `gram start` it needs rather than sharing wiring, so the tier's
// dependency graph stays small and explicit. It never constructs a Temporal
// client, so it keeps serving through a Temporal outage.
func newMCPCommand() *cli.Command {
	shutdown := &mcpServerShutdown{
		funcs:              nil,
		dbClose:            func() {},
		redisClose:         func() error { return nil },
		clickhouseShutdown: noopShutdown,
	}
	flags := mcpServerFlags()
	return &cli.Command{
		Name:  "mcp",
		Usage: "Start the MCP and OAuth serving tier (no Temporal dependency)",
		Flags: flags,
		Action: func(c *cli.Context) error {
			return runMCPServer(c, shutdown)
		},
		Before: func(ctx *cli.Context) error {
			return loadConfigFromFile(ctx, flags)
		},
		After: func(c *cli.Context) error {
			ctx := context.WithoutCancel(c.Context)
			logger := PullLogger(c.Context)
			// The datastore clients close last, after every registered
			// shutdown func has had the chance to flush through them.
			defer shutdown.dbClose()
			defer o11y.LogDefer(ctx, logger, "failed to close redis client", shutdown.redisClose)
			defer o11y.LogDefer(ctx, logger, "failed to shut down clickhouse client", func() error { return shutdown.clickhouseShutdown(ctx) })
			return runShutdown(logger, c.Context, shutdown.funcs)
		},
	}
}

// mcpServerShutdown collects what the action opens so the command's After
// hook can release it, mirroring `gram start`. funcs run concurrently through
// runShutdown; the datastore closers run afterwards in After.
type mcpServerShutdown struct {
	funcs              []func(context.Context) error
	dbClose            func()
	redisClose         func() error
	clickhouseShutdown func(context.Context) error
}

func runMCPServer(c *cli.Context, shutdown *mcpServerShutdown) error {
	const serviceName = "gram-mcp"
	serviceEnv := c.String("environment")
	appinfo := o11y.PullAppInfo(c.Context)
	appinfo.Command = "mcp"
	logger := PullLogger(c.Context).With(
		attr.SlogComponent("mcp"),
		attr.SlogServiceName(serviceName),
		attr.SlogServiceVersion(shortGitSHA()),
		attr.SlogServiceEnv(serviceEnv),
	)
	slog.SetDefault(logger)

	ctx, cancel := context.WithCancel(c.Context)
	defer cancel()

	otelShutdown, err := o11y.SetupOTelSDK(ctx, logger, o11y.SetupOTelSDKOptions{
		ServiceName:    serviceName,
		ServiceVersion: shortGitSHA(),
		GitSHA:         GitSHA,
		EnableTracing:  c.Bool("with-otel-tracing"),
		EnableMetrics:  c.Bool("with-otel-metrics"),
	})
	if err != nil {
		return fmt.Errorf("setup opentelemetry sdk: %w", err)
	}
	shutdown.funcs = append(shutdown.funcs, otelShutdown)
	tracerProvider, meterProvider := otel.GetTracerProvider(), otel.GetMeterProvider()

	db, err := newDBClient(ctx, logger, meterProvider, c.String("database-url"), dbClientOptions{enableUnsafeLogging: c.Bool("unsafe-db-log")})
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	shutdown.dbClose = db.Close
	if err := o11y.StartObservers(meterProvider, db); err != nil {
		return fmt.Errorf("start database observers: %w", err)
	}

	chDB, chShutdown, err := newClickhouseClient(ctx, logger, c)
	if err != nil {
		return fmt.Errorf("connect to clickhouse: %w", err)
	}
	shutdown.clickhouseShutdown = chShutdown

	redisClient, err := newRedisClient(ctx, redisClientOptions{
		redisAddr:     c.String("redis-cache-addr"),
		redisPassword: c.String("redis-cache-password"),
		enableTracing: c.Bool("redis-enable-tracing"),
	})
	if err != nil {
		return fmt.Errorf("connect to redis: %w", err)
	}
	shutdown.redisClose = redisClient.Close
	cacheImpl := cache.NewRedisCacheAdapter(redisClient)

	assetStorage, stop, err := newAssetStorage(ctx, logger, assetStorageOptions{assetsBackend: c.String("assets-backend"), assetsURI: c.String("assets-uri")})
	if err != nil {
		return fmt.Errorf("initialize asset storage: %w", err)
	}
	shutdown.funcs = append(shutdown.funcs, stop)

	guardianPolicy, err := newGuardianPolicy(c, logger, tracerProvider, meterProvider, redisClient)
	if err != nil {
		return err
	}
	identityDeps, err := newServerIdentity(ctx, c, logger, tracerProvider, db, redisClient, guardianPolicy)
	if err != nil {
		return err
	}
	posthogClient, featureFlags := identityDeps.Posthog, identityDeps.Features
	billingRepo, billingTracker := identityDeps.Billing, identityDeps.BillingTracker
	productFeatures, siteURL := identityDeps.ProductFeatures, identityDeps.SiteURL
	identityResolver, sessionManager, chatSessions := identityDeps.Identity, identityDeps.Sessions, identityDeps.ChatSessions

	serverURL, err := url.Parse(c.String("server-url"))
	if err != nil {
		return fmt.Errorf("parse server url: %w", err)
	}
	if err := validateServerURL(serverURL, serviceEnv); err != nil {
		return fmt.Errorf("invalid server url: %w", err)
	}
	callerAssertions, err := newCallerAssertions(c)
	if err != nil {
		return fmt.Errorf("configure caller assertions: %w", err)
	}

	authenticationHost, err := mcp.NewAuthenticationHost(c.String("authentication-host-url"), serverURL, serviceEnv)
	if err != nil {
		return fmt.Errorf("invalid authentication host url: %w", err)
	}

	enc, err := encryption.New(c.String("encryption-key"))
	if err != nil {
		return fmt.Errorf("create encryption client: %w", err)
	}
	env := environments.NewEnvironmentEntries(logger, db, enc, mcpmetadata_repo.New(db))
	auditLogger := newAuditLogger()

	// No key refresher: refreshing a provisioned key is a Temporal workflow,
	// and provisioning tolerates its absence.
	var openRouter openrouter.Provisioner
	if serviceEnv == "local" {
		openRouter = openrouter.NewDevelopment(c.String("openrouter-dev-key"))
	} else {
		openRouter = openrouter.New(logger, tracerProvider, guardianPolicy, db, serviceEnv, c.String("openrouter-provisioning-key"), nil, productFeatures, billingTracker, enc)
	}

	tigrisStore, stop, err := newTigrisStore(ctx, c, logger)
	if err != nil {
		return fmt.Errorf("create tigris asset store: %w", err)
	}
	shutdown.funcs = append(shutdown.funcs, stop)
	functionsOrchestrator, stop, err := newFunctionOrchestrator(c, logger, tracerProvider, guardianPolicy, db, assetStorage, tigrisStore, enc)
	if err != nil {
		return fmt.Errorf("create functions orchestrator: %w", err)
	}
	shutdown.funcs = append(shutdown.funcs, stop)

	roleClient, err := newAccessRoleProvider(ctx, logger, guardianPolicy, c)
	if err != nil {
		return fmt.Errorf("create access role provider: %w", err)
	}
	authzEngine := authz.NewEngine(logger, db,
		authz.ChallengeLoggingEnabled(newFeatureChecker(logger, productFeatures, productfeatures.FeatureAuthzChallengeLogging)),
		roleClient, authz.EngineOpts{
			AdmitPrincipalCredential:         runtimepolicy.AdmitPrincipalCredential,
			AdmitPrincipalCredentialWithDBTX: runtimepolicy.AdmitPrincipalCredentialWithDBTX,
			AdmitWorkloadSession:             runtimepolicy.AdmitWorkloadSession,
			DevMode:                          serviceEnv == "local",
		})

	_, psbroker, stop, err := newPubSubClient(ctx, c, logger)
	if err != nil {
		return fmt.Errorf("create pubsub client: %w", err)
	}
	shutdown.funcs = append(shutdown.funcs, stop)
	publishers, stop, err := newPublishers(ctx, psbroker)
	if err != nil {
		return fmt.Errorf("create publishers: %w", err)
	}
	publishersShutdown := len(shutdown.funcs)
	shutdown.funcs = append(shutdown.funcs, stop)

	logsEnabled := newFeatureChecker(logger, productFeatures, productfeatures.FeatureLogs)
	toolIOLogsEnabled := newFeatureChecker(logger, productFeatures, productfeatures.FeatureToolIOLogs)
	sessionCaptureEnabled := newFeatureChecker(logger, productFeatures, productfeatures.FeatureSessionCapture)
	telemLogger, stop := newTelemetryLogger(ctx, logger, tracerProvider, meterProvider, db, cacheImpl, chDB, logsEnabled, toolIOLogsEnabled,
		tm.NewLogPublisher(logger, tracerProvider, meterProvider, publishers.TelemetryLogs))
	shutdown.funcs = append(shutdown.funcs, stop)
	telemSvc := tm.NewService(logger, tracerProvider, db, chDB, sessionManager, chatSessions, logsEnabled, sessionCaptureEnabled, posthogClient, authzEngine, featureFlags)

	// Platform memory tools and dynamic tool search need a completions client.
	// The chat writer captures any transcript they produce but carries none of
	// the Temporal-backed observers `gram start` attaches.
	chatWriter, stop := chat.NewChatMessageWriter(logger, db, assetStorage)
	shutdown.funcs = append(shutdown.funcs, stop)
	completions := openrouter.NewUnifiedClient(logger, guardianPolicy, openRouter, modelkeys.NewResolver(db, enc, openRouter),
		chat.NewChatMessageCaptureStrategy(logger, meterProvider, db, chatWriter), chat.NewDefaultUsageTrackingStrategy(db, logger, billingTracker), nil, telemLogger)
	memoryService := memory.NewMemoryService(logger, tracerProvider, meterProvider, db, completions, auditLogger)
	ragService := rag.NewToolsetVectorStore(logger, tracerProvider, db, completions)
	shadowMCPClient := shadowmcp.NewClient(logger, db, cacheImpl, serverURL)
	mcpRiskEvaluator, mcpRiskScanner, err := newMCPRiskEvaluator(
		c, logger, tracerProvider, meterProvider, db, redisClient, featureFlags, completions, publishers, shadowMCPClient,
	)
	if err != nil {
		return err
	}
	shutdown.funcs = append(shutdown.funcs, mcpRiskScanner.Shutdown)
	// Shutdown funcs run concurrently, so flag findings drain inside the
	// publishers' stop instead of racing it.
	stopPublishers := shutdown.funcs[publishersShutdown]
	shutdown.funcs[publishersShutdown] = func(ctx context.Context) error {
		return errors.Join(mcpRiskEvaluator.Drain(ctx), stopPublishers(ctx))
	}
	slackClient := slack_client.NewSlackClient(guardianPolicy)
	// Listing and reading triggers works without Temporal; scheduling one
	// returns an error from the trigger tool instead of dispatching.
	triggerApp := newTriggersApp(logger, db, enc, nil, telemLogger, auditLogger, serverURL, siteURL, slackClient)
	assistantTokenManager := assistanttokens.New(c.String(usersessions.JWTSigningKeyFlag), db, authzEngine)
	platformExtras := append([]platformtools.ExternalTool{}, platformtoolsruntime.MemoryExternalTools(memoryService)...)
	platformExtras = append(platformExtras, platformtoolsruntime.AssistantSkillTools(logger, db)...)

	gcpIdentity := newGCPIdentity(ctx, logger, c)
	kmsSigningClients, err := newKMSSigningClients(ctx, logger, c)
	if err != nil {
		return fmt.Errorf("build kms signing client factory: %w", err)
	}
	clientAssertionSigner := remotesessions.NewKMSClientAssertionSigner(logger, db, gcpIdentity, kmsSigningClients)
	clientAssertionSigner.PinManagedSigner(c.String(identityProviderSigningServiceAccount))

	tunnelHTTPClient, err := newTunnelHTTPClient(c, guardianPolicy, redisClient)
	if err != nil {
		return fmt.Errorf("build tunnel http client: %w", err)
	}
	remoteSessionDeps, err := newMCPRemoteSessionDependencies(logger, tracerProvider, meterProvider, db, enc, guardianPolicy, tunnelHTTPClient, redisClient, serverURL, auditLogger, clientAssertionSigner)
	if err != nil {
		return err
	}
	mcpService, err := newMCPService(c, mcpServiceDependencies{
		CallerAssertions: callerAssertions,
		Logger:           logger, Tracer: tracerProvider, Meter: meterProvider, DB: db, Redis: redisClient,
		Sessions: sessionManager, ChatSessions: chatSessions, Environment: env,
		Posthog: posthogClient, Features: featureFlags, ServerURL: serverURL, SiteURL: siteURL,
		Encryption: enc, Guardian: guardianPolicy, Functions: functionsOrchestrator,
		BillingTracker: billingTracker, Billing: billingRepo, Telemetry: telemLogger, TelemetryService: telemSvc,
		RAG: ragService, Triggers: triggerApp, Authz: authzEngine, AssistantTokens: assistantTokenManager,
		ShadowMCP: shadowMCPClient, MCPRisk: mcpRiskEvaluator, Audit: auditLogger,
		PlatformExtras: platformExtras, PlatformFeatureChecker: productFeatures.PlatformFeatureCheck,
		PlatformToolsets: map[string]platformtools.Toolset{}, Identity: identityResolver, Challenges: remoteSessionDeps.Challenges,
	})
	if err != nil {
		return err
	}
	mcpService.StartRemoteSessionRecheck(ctx)

	// Private ingress expansion is never admitted here: the lifecycle
	// reconciler is a Temporal worker this tier does not run. Observation and
	// containment still resolve through the same admission checks.
	admission := networkingress.NewExpansionAdmission(productFeatures, false, c.Bool("network-ingress-enabled"))
	metadata := mcpmetadata.NewService(logger, tracerProvider, meterProvider, db, sessionManager, serverURL, siteURL, cacheImpl, authzEngine, auditLogger, admission.CheckExpansion)
	runtime, err := buildMCPServerRuntime(mcpServerRuntimeDependencies{Logger: logger, DB: db, Encryption: enc, MCP: mcpService, Metadata: metadata})
	if err != nil {
		return fmt.Errorf("build MCP server runtime: %w", err)
	}

	mux, err := newMCPServerMux(c, logger, db, serverURL, authenticationHost, chatSessions, publishers)
	if err != nil {
		return err
	}
	mcp.Attach(mux, runtime.MCP, runtime.Metadata)
	xmcp.Attach(mux, runtime.XMCP, runtime.Metadata)
	mcp.AttachAuthenticationHost(authenticationHost, runtime.MCP)
	xmcp.AttachAuthenticationHost(authenticationHost, runtime.XMCP)
	usersessions.AttachRetiredProxy(mux, logger)

	srv := &http.Server{
		Addr:              c.String("address"),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		// Mirrors `gram start`: the fronting load balancer retires idle
		// backend connections at 600s, so the backend must outlast it. No
		// WriteTimeout, since this mux serves long-lived MCP streams.
		IdleTimeout: 620 * time.Second,
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	sigctx, sigcancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer sigcancel()
	group := pool.New()
	group.Go(func() {
		<-sigctx.Done()
		logger.InfoContext(ctx, "shutting down mcp server")
		graceCtx, graceCancel := context.WithTimeoutCause(context.WithoutCancel(ctx), shutdownDrainTimeout, errors.New("graceful shutdown timed out"))
		defer graceCancel()
		if err := srv.Shutdown(graceCtx); err != nil {
			if gerr := context.Cause(graceCtx); gerr != nil {
				err = errors.Join(err, gerr)
			}
			logger.ErrorContext(ctx, "failed to shutdown mcp server", attr.SlogError(err))
		}
		// Handlers are drained; the automatic remote-session probes they
		// detached still write verdicts, so give them their own budget before
		// the datastores close.
		drainCtx, cancelDrain := context.WithTimeout(context.WithoutCancel(ctx), probeDrainTimeout)
		defer cancelDrain()
		if err := mcpService.Shutdown(drainCtx); err != nil {
			logger.ErrorContext(ctx, "drain automatic remote session verifications", attr.SlogError(err))
		}
	})

	stopControl, err := startMCPControlServer(ctx, c, logger, srv.Addr, db, redisClient)
	if err != nil {
		return err
	}
	shutdown.funcs = append(shutdown.funcs, stopControl)

	tlsEnabled := c.String("ssl-key-file") != "" && c.String("ssl-cert-file") != ""
	var serveErr error
	if tlsEnabled {
		logger.InfoContext(ctx, "mcp server started with tls", attr.SlogServerAddress(srv.Addr))
		serveErr = srv.ListenAndServeTLS(c.String("ssl-cert-file"), c.String("ssl-key-file"))
	} else {
		logger.InfoContext(ctx, "mcp server started", attr.SlogServerAddress(srv.Addr))
		serveErr = srv.ListenAndServe()
	}
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil // Shutdown was requested; the drain below is the normal exit.
	}
	if serveErr != nil {
		logger.ErrorContext(ctx, "mcp server error", attr.SlogError(serveErr))
		sigcancel()
	}
	// ListenAndServe returns the instant Shutdown is called. Wait for the drain
	// before cancelling ctx, which is the server's BaseContext.
	group.Wait()
	remoteSessionDeps.Refresher.Shutdown()
	cancel()
	if serveErr != nil {
		return fmt.Errorf("serve mcp: %w", serveErr)
	}
	return nil
}

// newMCPServerMux builds the public listener middleware chain for the MCP
// tier. It mirrors the public-route portion of the `gram start` chain and
// omits the marketplace, hooks, and management-API layers.
func newMCPServerMux(c *cli.Context, logger *slog.Logger, db *pgxpool.Pool, serverURL *url.URL, authenticationHost *mcp.AuthenticationHost, chatSessions middleware.ChatSessionValidator, publishers *background.Publishers) (goahttp.Muxer, error) {
	mux := goahttp.NewMuxer()
	mux.Use(middleware.NetworkServingPolicyVersion)
	mux.Use(mcpauthz.StripMiddleware)
	mux.Use(middleware.StripPrivateIngressHeaders)
	mux.Use(func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.Path == "/healthz" {
				w.WriteHeader(http.StatusOK)
				return
			}
			h.ServeHTTP(w, r)
		})
	})
	mux.Use(middleware.DropInboundOTelBaggage)
	mux.Use(func(h http.Handler) http.Handler {
		return otelhttp.NewHandler(h, "http",
			otelhttp.WithServerName("gram-mcp"),
			otelhttp.WithPublicEndpointFn(middleware.IsOTelPublicEndpoint),
		)
	})
	mux.Use(middleware.RouteLabelerMiddleware)
	mux.Use(middleware.MCPProtocolVersionTelemetry)
	mux.Use(middleware.NewHTTPLoggingMiddleware(logger))
	mux.Use(middleware.NewRecovery(logger))
	platformHosts, err := parsePlatformHosts(c, authenticationHost)
	if err != nil {
		return nil, err
	}
	mux.Use(middleware.CORSMiddleware(c.String("environment"), c.String("server-url"), platformOrigins(platformHosts), chatSessions))
	mcpSecurity, err := middleware.MCPSecurity(logger, append([]string{c.String("server-url"), c.String("site-url")}, platformOrigins(platformHosts)...))
	if err != nil {
		return nil, fmt.Errorf("configure mcp security middleware: %w", err)
	}
	// Below CORS, which browser OAuth clients need on the authentication
	// host too. Above MCPSecurity, so MCP endpoint paths answer 404 there like
	// every route the host does not serve, and above customdomains.Middleware,
	// which refuses hosts it does not know.
	mux.Use(authenticationHost.Middleware)
	mux.Use(mcpSecurity)
	mux.Use(customdomains.Middleware(logger, db, c.String("environment"), serverURL, platformHosts))
	mux.Use(metering.NewMCPBandwidthMiddleware(logger, publishers.MeterReadings))
	mux.Use(middleware.SessionMiddleware)
	return mux, nil
}

// startMCPControlServer exposes readiness for the tier: the API listener, the
// database, and Redis. There is no Temporal resource to report.
func startMCPControlServer(ctx context.Context, c *cli.Context, logger *slog.Logger, listenAddr string, db *pgxpool.Pool, redisClient *redis.Client) (func(context.Context) error, error) {
	tlsEnabled := c.String("ssl-key-file") != "" && c.String("ssl-cert-file") != ""
	if listenAddr == "" {
		listenAddr = ":8080"
	}
	host, port, _ := net.SplitHostPort(listenAddr)
	if host == "" {
		host = "localhost"
	}
	healthzEndpoint := &o11y.HTTPEndpoint{
		URL: &url.URL{
			Scheme: conv.Ternary(tlsEnabled, "https", "http"),
			Host:   net.JoinHostPort(host, port),
			Path:   "/healthz",
		},
		TLSCertificate: nil,
	}
	if tlsEnabled {
		cert, err := os.ReadFile(c.String("ssl-cert-file"))
		if err != nil {
			return nil, fmt.Errorf("read TLS certificate for health check: %w", err)
		}
		healthzEndpoint.TLSCertificate = cert
	}
	controlServer := control.Server{
		Address:          c.String("control-address"),
		Logger:           logger.With(attr.SlogComponent("control")),
		DisableProfiling: false,
	}
	stop, err := controlServer.Start(ctx, o11y.NewHealthCheckHandler(
		[]*o11y.NamedResource[*o11y.HTTPEndpoint]{{Name: "api", Resource: healthzEndpoint}},
		[]*o11y.NamedResource[*pgxpool.Pool]{{Name: "default", Resource: db}},
		[]*o11y.NamedResource[*redis.Client]{{Name: "default", Resource: redisClient}},
		[]*o11y.NamedResource[client.Client]{},
	))
	if err != nil {
		return nil, fmt.Errorf("start control server: %w", err)
	}
	return stop, nil
}
