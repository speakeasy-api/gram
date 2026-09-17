package gram

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"slices"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"

	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/assistants"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata"
	metadatarepo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/memory"
	"github.com/speakeasy-api/gram/server/internal/modelkeys"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	platformruntime "github.com/speakeasy-api/gram/server/internal/platformtools/runtime"
	platformskills "github.com/speakeasy-api/gram/server/internal/platformtools/skills"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/rag"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/skills/efficacy"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	slackapi "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/api"
	slackclient "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
)

type privateIngressRuntime struct {
	DB         *pgxpool.Pool
	Redis      *redis.Client
	Temporal   *tenv.Environment
	Kubernetes *k8s.KubernetesClients
	Runtime    *mcpServerRuntime
	cleanup    []func(context.Context) error
}

func (r *privateIngressRuntime) Close(ctx context.Context) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownDrainTimeout)
	defer cancel()
	for _, v := range slices.Backward(r.cleanup) {
		_ = o11y.LogDefer(ctx, slog.Default(), "close private ingress dependency", func() error { return v(ctx) })
	}
}

func validatePrivateIngressTemporalConfig(c *cli.Context) error {
	if c.String("temporal-address") == "" || c.String("temporal-namespace") == "" || c.String("temporal-task-queue") == "" {
		return fmt.Errorf("private ingress Temporal address, namespace, and task queue are required")
	}
	return validateNetworkIngressWorkerTemporalTLS(c.String("environment"), c.String("temporal-client-cert"), c.String("temporal-client-key"))
}

func newPrivateIngressRuntime(ctx context.Context, c *cli.Context, logger *slog.Logger) (_ *privateIngressRuntime, err error) {
	if err := validatePrivateIngressTemporalConfig(c); err != nil {
		return nil, err
	}
	r := &privateIngressRuntime{DB: nil, Redis: nil, Temporal: nil, Kubernetes: nil, Runtime: nil, cleanup: nil}
	defer func() {
		if err != nil {
			r.Close(ctx)
		}
	}()
	tracerProvider, meterProvider := otel.GetTracerProvider(), otel.GetMeterProvider()
	db, err := newDBClient(ctx, logger, meterProvider, c.String("database-url"), dbClientOptions{enableUnsafeLogging: c.Bool("unsafe-db-log")})
	if err != nil {
		return nil, err
	}
	r.DB = db
	r.cleanup = append(r.cleanup, func(context.Context) error { db.Close(); return nil })
	chDB, stop, err := newClickhouseClient(ctx, logger, c)
	if err != nil {
		return nil, err
	}
	r.cleanup = append(r.cleanup, stop)
	if err := o11y.StartObservers(meterProvider, db); err != nil {
		return nil, fmt.Errorf("start private ingress database observers: %w", err)
	}
	assetStorage, stop, err := newAssetStorage(ctx, logger, assetStorageOptions{assetsBackend: c.String("assets-backend"), assetsURI: c.String("assets-uri")})
	if err != nil {
		return nil, err
	}
	r.cleanup = append(r.cleanup, stop)
	redisClient, err := newRedisClient(ctx, redisClientOptions{redisAddr: c.String("redis-cache-addr"), redisPassword: c.String("redis-cache-password"), enableTracing: c.Bool("redis-enable-tracing")})
	if err != nil {
		return nil, err
	}
	r.Redis = redisClient
	r.cleanup = append(r.cleanup, func(context.Context) error { return redisClient.Close() })
	cacheImpl := cache.NewRedisCacheAdapter(redisClient)
	guardianPolicy, err := newGuardianPolicy(c, logger, tracerProvider, meterProvider, redisClient)
	if err != nil {
		return nil, err
	}
	identityDeps, err := newServerIdentity(ctx, c, logger, tracerProvider, db, redisClient, guardianPolicy)
	if err != nil {
		return nil, err
	}
	posthogClient, featureFlags := identityDeps.Posthog, identityDeps.Features
	billingRepo, billingTracker := identityDeps.Billing, identityDeps.BillingTracker
	productFeatures, siteURL := identityDeps.ProductFeatures, identityDeps.SiteURL
	identityResolver, sessionManager, chatSessions := identityDeps.Identity, identityDeps.Sessions, identityDeps.ChatSessions
	serverURL, err := url.Parse(c.String("server-url"))
	if err != nil {
		return nil, fmt.Errorf("parse server URL: %w", err)
	}
	if err := validateServerURL(serverURL, c.String("environment")); err != nil {
		return nil, err
	}

	enc, err := encryption.New(c.String("encryption-key"))
	if err != nil {
		return nil, fmt.Errorf("create private ingress encryption client: %w", err)
	}
	env := environments.NewEnvironmentEntries(logger, db, enc, metadatarepo.New(db))
	r.Kubernetes, err = k8s.InitializeK8sClient(ctx, logger, c.String("environment"), "", "")
	if err != nil {
		return nil, fmt.Errorf("create private ingress Kubernetes client: %w", err)
	}
	if r.Kubernetes.Clientset == nil {
		return nil, fmt.Errorf("private network ingress listener requires an in-cluster Kubernetes client")
	}
	r.Temporal, stop, err = newTemporalClient(logger, meterProvider, temporalClientOptions{
		address: c.String("temporal-address"), namespace: c.String("temporal-namespace"), taskQueue: c.String("temporal-task-queue"),
		certPEMBlock: []byte(c.String("temporal-client-cert")), keyPEMBlock: []byte(c.String("temporal-client-key")),
	})
	if err != nil {
		return nil, err
	}
	if r.Temporal != nil {
		r.cleanup = append(r.cleanup, stop)
	}
	auditLogger := newAuditLogger()
	var openRouter openrouter.Provisioner
	if c.String("environment") == "local" {
		openRouter = openrouter.NewDevelopment(c.String("openrouter-dev-key"))
	} else {
		openRouter = openrouter.New(logger, tracerProvider, guardianPolicy, db, c.String("environment"), c.String("openrouter-provisioning-key"), &background.OpenRouterKeyRefresher{TemporalEnv: r.Temporal}, productFeatures, billingTracker, enc)
	}
	tigrisStore, stop, err := newTigrisStore(ctx, c, logger)
	if err != nil {
		return nil, err
	}
	r.cleanup = append(r.cleanup, stop)
	functionsOrchestrator, stop, err := newFunctionOrchestrator(c, logger, tracerProvider, guardianPolicy, db, assetStorage, tigrisStore, enc)
	if err != nil {
		return nil, err
	}
	r.cleanup = append(r.cleanup, stop)
	roleClient, err := newAccessRoleProvider(ctx, logger, guardianPolicy, c)
	if err != nil {
		return nil, err
	}
	authzEngine := authz.NewEngine(logger, db, authz.ChallengeLoggingEnabled(newFeatureChecker(logger, productFeatures, productfeatures.FeatureAuthzChallengeLogging)), roleClient, authz.EngineOpts{
		AdmitPrincipalCredential: runtimepolicy.AdmitPrincipalCredential, AdmitPrincipalCredentialWithDBTX: runtimepolicy.AdmitPrincipalCredentialWithDBTX,
		AdmitWorkloadSession: runtimepolicy.AdmitWorkloadSession,
		DevMode:              c.String("environment") == "local",
	})
	_, broker, stop, err := newPubSubClient(ctx, c, logger)
	if err != nil {
		return nil, err
	}
	r.cleanup = append(r.cleanup, stop)
	publishers, stop, err := newPublishers(ctx, broker)
	if err != nil {
		return nil, err
	}
	r.cleanup = append(r.cleanup, stop)
	logsEnabled := newFeatureChecker(logger, productFeatures, productfeatures.FeatureLogs)
	telemLogger, stop := newTelemetryLogger(ctx, logger, tracerProvider, meterProvider, db, cacheImpl, chDB, logsEnabled,
		newFeatureChecker(logger, productFeatures, productfeatures.FeatureToolIOLogs), tm.NewLogPublisher(logger, tracerProvider, meterProvider, publishers.TelemetryLogs))
	r.cleanup = append(r.cleanup, stop)
	telemSvc := tm.NewService(logger, tracerProvider, db, chDB, sessionManager, chatSessions, logsEnabled, newFeatureChecker(logger, productFeatures, productfeatures.FeatureSessionCapture), posthogClient, authzEngine, featureFlags)
	chatWriter, stop := chat.NewChatMessageWriter(logger, db, assetStorage)
	r.cleanup = append(r.cleanup, stop)
	chatWriter = chatWriter.WithTurnStream(chat.NewTurnStream(redisClient))
	efficacySignaler := background.NewThrottledSignaler(&background.TemporalSkillEfficacySignaler{TemporalEnv: r.Temporal, Logger: logger}, background.SkillEfficacySignalCooldown, logger)
	analysisSignaler := background.NewThrottledSignaler(&background.TemporalChatAnalysisSignaler{TemporalEnv: r.Temporal, Logger: logger}, background.ChatAnalysisSignalCooldown, logger)
	r.cleanup = append(r.cleanup, efficacySignaler.Shutdown, analysisSignaler.Shutdown)
	chatWriter.AddObserver(efficacy.NewObserver(logger, efficacySignaler))
	chatWriter.AddObserver(analysis.NewObserver(logger, analysisSignaler))
	completions := openrouter.NewUnifiedClient(logger, guardianPolicy, openRouter, modelkeys.NewResolver(db, enc, openRouter), chat.NewChatMessageCaptureStrategy(logger, meterProvider, db, chatWriter), chat.NewDefaultUsageTrackingStrategy(db, logger, billingTracker), &background.TemporalChatTitleGenerator{TemporalEnv: r.Temporal}, telemLogger)
	memoryService := memory.NewMemoryService(logger, tracerProvider, meterProvider, db, completions, auditLogger)
	ragService := rag.NewToolsetVectorStore(logger, tracerProvider, db, completions)
	slackClient := slackclient.NewSlackClient(guardianPolicy)
	triggerApp := newTriggersApp(logger, db, enc, r.Temporal, telemLogger, auditLogger, serverURL, siteURL, slackClient)
	assistantTokenManager := assistanttokens.New(c.String(usersessions.JWTSigningKeyFlag), db, authzEngine)
	assistantRuntime, err := newAssistantRuntime(ctx, logger, tracerProvider, c, guardianPolicy, db, serverURL)
	if err != nil {
		return nil, err
	}
	contextWindowResolver := openrouter.NewContextWindowResolver(logger, guardianPolicy, cacheImpl)
	assistantsCore := assistants.NewServiceCore(logger, tracerProvider, meterProvider, db, guardianPolicy, enc, assistantRuntime, slackClient, assistantTokenManager, serverURL, telemLogger, contextWindowResolver, auditLogger)
	assistantsCore.SetWakeCanceller(triggerApp)
	assistantsCore.SetDashboardIngestor(triggerApp)
	assistantsCore.SetChatMessageWriter(chatWriter)
	assistantsCore.SetAssetStorage(assetStorage)
	assistantsCore.SetAssetSigningKey(c.String(usersessions.JWTSigningKeyFlag))
	assistantsCore.SetSlackImageInlining(env, slackapi.NewClient("", guardianPolicy.PooledClient()))
	assistantsCore.SetFeatureProvider(featureFlags)
	triggerApp.RegisterDispatcher(assistants.NewService(logger, tracerProvider, meterProvider, db, sessionManager, authzEngine, assistantsCore, &background.AssistantWorkflowSignaler{TemporalEnv: r.Temporal}, ratelimit.NewRedisStore(redisClient)))
	platformExtras := append([]platformtools.ExternalTool{}, platformruntime.MemoryExternalTools(memoryService)...)
	platformExtras = append(platformExtras, platformruntime.AssistantSkillTools(logger, db, platformskills.WithEfficacySignaler(efficacySignaler))...)
	gcpIdentity := newGCPIdentity(ctx, logger, c)
	kmsSigningClients, err := newKMSSigningClients(ctx, logger, c)
	if err != nil {
		return nil, fmt.Errorf("build kms signing client factory: %w", err)
	}
	clientAssertionSigner := remotesessions.NewKMSClientAssertionSigner(logger, db, gcpIdentity, kmsSigningClients)
	tunnelHTTPClient, err := newTunnelHTTPClient(c, guardianPolicy, redisClient)
	if err != nil {
		return nil, fmt.Errorf("build tunnel http client: %w", err)
	}
	remoteSessionDeps, err := newMCPRemoteSessionDependencies(logger, tracerProvider, meterProvider, db, enc, guardianPolicy, tunnelHTTPClient, redisClient, serverURL, auditLogger, clientAssertionSigner)
	if err != nil {
		return nil, err
	}
	r.cleanup = append(r.cleanup, func(context.Context) error { remoteSessionDeps.Refresher.Shutdown(); return nil })
	challengeManager := remoteSessionDeps.Challenges
	mcpService, err := newMCPService(c, mcpServiceDependencies{
		Logger: logger, Tracer: tracerProvider, Meter: meterProvider, DB: db, Redis: redisClient,
		Sessions: sessionManager, ChatSessions: chatSessions, Environment: env,
		Posthog: posthogClient, Features: featureFlags, ServerURL: serverURL, SiteURL: siteURL,
		Encryption: enc, Guardian: guardianPolicy, Functions: functionsOrchestrator,
		BillingTracker: billingTracker, Billing: billingRepo, Telemetry: telemLogger, TelemetryService: telemSvc,
		RAG: ragService, Triggers: triggerApp, Authz: authzEngine, AssistantTokens: assistantTokenManager,
		ShadowMCP: shadowmcp.NewClient(logger, db, cacheImpl, serverURL), Audit: auditLogger,
		PlatformExtras: platformExtras, PlatformFeatureChecker: productFeatures.PlatformFeatureCheck,
		PlatformToolsets: map[string]platformtools.Toolset{}, Identity: identityResolver, Challenges: challengeManager,
	})
	if err != nil {
		return nil, err
	}
	r.cleanup = append(r.cleanup, func(ctx context.Context) error {
		drainCtx, cancel := context.WithTimeout(ctx, probeDrainTimeout)
		defer cancel()
		return mcpService.Shutdown(drainCtx)
	})
	mcpService.StartRemoteSessionRecheck(ctx)
	admission := networkingress.NewExpansionAdmission(productFeatures, featureFlags, orgrepo.New(db), false, c.Bool("network-ingress-enabled"))
	metadata := mcpmetadata.NewService(logger, tracerProvider, meterProvider, db, sessionManager, serverURL, siteURL, cacheImpl, authzEngine, auditLogger, admission.CheckExpansion)
	r.Runtime, err = buildMCPServerRuntime(mcpServerRuntimeDependencies{Logger: logger, DB: db, Encryption: enc, MCP: mcpService, Metadata: metadata})
	if err != nil {
		return nil, err
	}
	return r, nil
}
