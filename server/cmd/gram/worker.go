package gram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
	"go.opentelemetry.io/otel"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/speakeasy-api/gram/server/internal/assistants"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/auth/speakeasyclient"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/background"
	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/control"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpclient"
	mcpmetadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oauth"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/rag"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/pylon"
	slack_client "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func newWorkerCommand() *cli.Command {
	var shutdownFuncs []func(context.Context) error

	flags := []cli.Flag{
		&cli.StringFlag{
			Name:     "server-url",
			Usage:    "The public URL of the server",
			EnvVars:  []string{"GRAM_SERVER_URL"},
			Required: true,
		},
		&cli.StringFlag{
			Name:     "environment",
			Usage:    "The current server environment", // local, dev, prod
			Required: true,
			EnvVars:  []string{"GRAM_ENVIRONMENT"},
		},
		&cli.StringFlag{
			Name:    "temporal-address",
			Usage:   "The address of the temporal server",
			EnvVars: []string{"TEMPORAL_ADDRESS"},
			Value:   "localhost:7233",
		},
		&cli.StringFlag{
			Name:    "temporal-namespace",
			Usage:   "The temporal namespace to use",
			EnvVars: []string{"TEMPORAL_NAMESPACE"},
			Value:   "default",
		},
		&cli.StringFlag{
			Name:    "temporal-task-queue",
			Usage:   "Task queue of the Temporal server",
			EnvVars: []string{"TEMPORAL_TASK_QUEUE"},
			Value:   "main",
		},
		&cli.StringFlag{
			Name:    "temporal-client-cert",
			Usage:   "Client cert of the Temporal server",
			EnvVars: []string{"TEMPORAL_CLIENT_CERT"},
		},
		&cli.StringFlag{
			Name:    "temporal-client-key",
			Usage:   "Client key of the Temporal server",
			EnvVars: []string{"TEMPORAL_CLIENT_KEY"},
		},
		&cli.StringFlag{
			Name:    "control-address",
			Value:   ":8081",
			Usage:   "HTTP address to listen on",
			EnvVars: []string{"GRAM_WORKER_CONTROL_ADDRESS"},
		},
		&cli.StringFlag{
			Name:     "database-url",
			Usage:    "Database URL",
			EnvVars:  []string{"GRAM_DATABASE_URL"},
			Required: true,
		},
		&cli.BoolFlag{
			Name:    "unsafe-db-log",
			Usage:   "Turn on unsafe database logging. WARNING: This will log all database queries and data to the console.",
			EnvVars: []string{"GRAM_UNSAFE_DB_LOG"},
			Value:   false,
		},
		&cli.BoolFlag{
			Name:    "with-otel-tracing",
			Usage:   "Enable OpenTelemetry traces",
			EnvVars: []string{"GRAM_ENABLE_OTEL_TRACES"},
		},
		&cli.BoolFlag{
			Name:    "with-otel-metrics",
			Usage:   "Enable OpenTelemetry metrics",
			EnvVars: []string{"GRAM_ENABLE_OTEL_METRICS"},
		},
		&cli.StringFlag{
			Name:     "assets-backend",
			Usage:    "The backend to use for managing assets",
			EnvVars:  []string{"GRAM_ASSETS_BACKEND"},
			Required: true,
			Action: func(c *cli.Context, val string) error {
				if val != "fs" && val != "gcs" {
					return fmt.Errorf("invalid assets backend: %s", val)
				}
				return nil
			},
		},
		&cli.StringFlag{
			Name:     "assets-uri",
			Usage:    "The location of the assets backend to connect to",
			EnvVars:  []string{"GRAM_ASSETS_URI"},
			Required: true,
		},
		&cli.StringFlag{
			Name:     "encryption-key",
			Usage:    "Key for App level AES encryption/decyryption",
			Required: true,
			EnvVars:  []string{"GRAM_ENCRYPTION_KEY"},
		},
		&cli.StringFlag{
			Name:     "slack-client-secret",
			Usage:    "The slack client secret",
			EnvVars:  []string{"SLACK_CLIENT_SECRET"},
			Required: false,
		},
		&cli.StringFlag{
			Name:    "openrouter-dev-key",
			Usage:   "Dev API key for OpenRouter (primarily for local development) - https://openrouter.ai/settings/keys",
			EnvVars: []string{"OPENROUTER_DEV_KEY"},
		},
		&cli.StringFlag{
			Name:    "openrouter-provisioning-key",
			Usage:   "Provisioning key for OpenRouter to create new API keys for orgs - https://openrouter.ai/settings/provisioning-keys",
			EnvVars: []string{"OPENROUTER_PROVISIONING_KEY"},
		},
		&cli.StringSliceFlag{
			Name:     "disallowed-cidr-blocks",
			Usage:    "List of CIDR blocks to block for SSRF protection",
			EnvVars:  []string{"GRAM_DISALLOWED_CIDR_BLOCKS"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "posthog-endpoint",
			Usage:    "The endpoint to proxy product metrics too",
			EnvVars:  []string{"POSTHOG_ENDPOINT"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "posthog-api-key",
			Usage:    "The posthog public API key",
			EnvVars:  []string{"POSTHOG_API_KEY"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "posthog-personal-api-key",
			Usage:    "The posthog personal API key for local feature flag evaluation",
			EnvVars:  []string{"POSTHOG_PERSONAL_API_KEY"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "local-feature-flags-csv",
			Usage:    "Path to a CSV file containing local feature flags. Format: distinct_id,flag,enabled (with header row).",
			EnvVars:  []string{"GRAM_LOCAL_FEATURE_FLAGS_CSV"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "polar-api-key",
			Usage:    "The polar API key",
			EnvVars:  []string{"POLAR_API_KEY"},
			Required: false,
		},
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:     "polar-product-id-free",
			Aliases:  []string{"polar.product_id_basic"},
			Usage:    "The product ID of the free tier in Polar",
			EnvVars:  []string{"POLAR_PRODUCT_ID_FREE"},
			Required: false,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:     "polar-product-id-pro",
			Aliases:  []string{"polar.product_id_pro"},
			Usage:    "The product ID of the pro tier in Polar",
			EnvVars:  []string{"POLAR_PRODUCT_ID_PRO"},
			Required: false,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:     "polar-meter-id-tool-calls",
			Aliases:  []string{"polar.meter_id_tool_calls"},
			Usage:    "The ID of the tool calls meter in Polar",
			EnvVars:  []string{"POLAR_METER_ID_TOOL_CALLS"},
			Required: false,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:     "polar-meter-id-servers",
			Aliases:  []string{"polar.meter_id_servers"},
			Usage:    "The ID of the servers meter in Polar",
			EnvVars:  []string{"POLAR_METER_ID_SERVERS"},
			Required: false,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:     "polar-meter-id-credits",
			Aliases:  []string{"polar.meter_id_credits"},
			Usage:    "The ID of the credits meter in Polar",
			EnvVars:  []string{"POLAR_METER_ID_CREDITS"},
			Required: false,
		}),
		&cli.StringSliceFlag{
			Name:     "polar-product-ids-topup",
			Usage:    "Product IDs of one-time credit top-up packs in Polar",
			EnvVars:  []string{"POLAR_PRODUCT_IDS_TOPUP"},
			Required: false,
		},
		&cli.StringFlag{
			Name:    "custom-domain-cname",
			Usage:   "The expected CNAME target for custom domain verification (e.g., cname.getgram.ai.)",
			EnvVars: []string{"GRAM_CUSTOM_DOMAIN_CNAME"},
		},
		&cli.StringFlag{
			Name:     "pylon-verification-secret",
			Usage:    "The identity verification secret for pylon",
			EnvVars:  []string{"PYLON_VERIFICATION_SECRET"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "speakeasy-server-address",
			Usage:    "Speakeasy server address",
			EnvVars:  []string{"SPEAKEASY_SERVER_ADDRESS"},
			Required: true,
		},
		&cli.StringFlag{
			Name:     "speakeasy-secret-key",
			Usage:    "Speakeasy secret key",
			EnvVars:  []string{"SPEAKEASY_SECRET_KEY"},
			Required: true,
		},
		&cli.StringFlag{
			Name:     "jwt-signing-key",
			Usage:    "Key for JWT signing",
			Required: true,
			EnvVars:  []string{"GRAM_JWT_SIGNING_KEY"},
		},
		&cli.PathFlag{
			Name:     "config-file",
			Usage:    "Path to a config file to load. Supported formats are JSON, TOML and YAML.",
			EnvVars:  []string{"GRAM_CONFIG_FILE"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "workos-api-key",
			Usage:    "WorkOS API key for the events client",
			EnvVars:  []string{"WORKOS_API_KEY"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "workos-endpoint",
			Usage:    "Base URL for WorkOS API calls. Leave unset for production (defaults to https://api.workos.com); set to the dev-idp's local-speakeasy mode for fully-local development.",
			EnvVars:  []string{"WORKOS_API_URL"},
			Required: false,
		},
		&cli.StringFlag{
			Name:    "presidio-analyzer-url",
			Usage:   "Base URL of the Presidio Analyzer service (e.g. http://presidio-analyzer:3000). Empty disables PII scanning.",
			EnvVars: []string{"PRESIDIO_ANALYZER_URL"},
		},
	}

	flags = append(flags, redisFlags...)
	flags = append(flags, clickHouseFlags...)
	flags = append(flags, functionsFlags...)
	flags = append(flags, pulseMCPFlags...)
	flags = append(flags, assistantRuntimeFlags...)

	return &cli.Command{
		Name:  "worker",
		Usage: "Start the temporal worker",
		Flags: flags,
		Action: func(c *cli.Context) error {
			serviceName := "gram-worker"
			serviceEnv := c.String("environment")
			appinfo := o11y.PullAppInfo(c.Context)
			appinfo.Command = "worker"
			logger := PullLogger(c.Context).With(
				attr.SlogComponent("worker"),
				attr.SlogServiceName(serviceName),
				attr.SlogServiceVersion(shortGitSHA()),
				attr.SlogServiceEnv(serviceEnv),
			)
			tracerProvider := otel.GetTracerProvider()
			meterProvider := otel.GetMeterProvider()
			slog.SetDefault(logger)

			ctx, cancel := context.WithCancel(c.Context)
			defer cancel()

			temporalEnv, shutdown, err := newTemporalClient(logger, temporalClientOptions{
				address:      c.String("temporal-address"),
				namespace:    c.String("temporal-namespace"),
				taskQueue:    c.String("temporal-task-queue"),
				certPEMBlock: []byte(c.String("temporal-client-cert")),
				keyPEMBlock:  []byte(c.String("temporal-client-key")),
			})
			if err != nil {
				return err
			}
			if temporalEnv == nil {
				return errors.New("insufficient options to create temporal client")
			}
			shutdownFuncs = append(shutdownFuncs, shutdown)

			shutdown, err = o11y.SetupOTelSDK(ctx, logger, o11y.SetupOTelSDKOptions{
				ServiceName:    serviceName,
				ServiceVersion: shortGitSHA(),
				GitSHA:         GitSHA,
				EnableTracing:  c.Bool("with-otel-tracing"),
				EnableMetrics:  c.Bool("with-otel-metrics"),
			})
			if err != nil {
				return fmt.Errorf("failed to setup opentelemetry sdk: %w", err)
			}
			shutdownFuncs = append(shutdownFuncs, shutdown)

			guardianPolicy, err := newGuardianPolicy(c, tracerProvider)
			if err != nil {
				return err
			}

			db, err := newDBClient(ctx, logger, meterProvider, c.String("database-url"), dbClientOptions{
				enableUnsafeLogging: c.Bool("unsafe-db-log"),
			})
			if err != nil {
				return err
			}
			// Ping the database to ensure connectivity
			if err := db.Ping(ctx); err != nil {
				logger.ErrorContext(ctx, "failed to ping database", attr.SlogError(err))
				return fmt.Errorf("database ping failed: %w", err)
			}
			defer db.Close()

			redisClient, err := newRedisClient(ctx, redisClientOptions{
				redisAddr:     c.String("redis-cache-addr"),
				redisPassword: c.String("redis-cache-password"),
				enableTracing: c.Bool("redis-enable-tracing"),
			})
			if err != nil {
				return fmt.Errorf("failed to connect to redis: %w", err)
			}

			encryptionClient, err := encryption.New(c.String("encryption-key"))
			if err != nil {
				return fmt.Errorf("failed to create encryption client: %w", err)
			}

			auditLogger := newAuditLogger()

			mcpMetadataRepo := mcpmetadata_repo.New(db)
			env := environments.NewEnvironmentEntries(logger, db, encryptionClient, mcpMetadataRepo)

			k8sClient, err := k8s.InitializeK8sClient(ctx, logger, c.String("environment"))
			if err != nil {
				return fmt.Errorf("failed to create k8s client: %w", err)
			}

			assetStorage, shutdown, err := newAssetStorage(ctx, logger, assetStorageOptions{
				assetsBackend: c.String("assets-backend"),
				assetsURI:     c.String("assets-uri"),
			})
			if err != nil {
				return fmt.Errorf("failed to create asset storage: %w", err)
			}
			shutdownFuncs = append(shutdownFuncs, shutdown)

			posthogClient := posthog.New(ctx, logger, c.String("posthog-api-key"), c.String("posthog-endpoint"), c.String("posthog-personal-api-key"))
			var featureFlags feature.Provider = posthogClient
			if c.String("environment") == "local" {
				featureFlags = newLocalFeatureFlags(ctx, logger, c.String("local-feature-flags-csv"))
			}

			{
				controlServer := control.Server{
					Address:          c.String("control-address"),
					Logger:           logger.With(attr.SlogComponent("control")),
					DisableProfiling: false,
				}

				shutdown, err := controlServer.Start(c.Context, o11y.NewHealthCheckHandler(
					[]*o11y.NamedResource[*o11y.HTTPEndpoint]{},
					[]*o11y.NamedResource[*pgxpool.Pool]{{Name: "default", Resource: db}},
					nil,
					[]*o11y.NamedResource[client.Client]{{Name: "default", Resource: temporalEnv.Client()}},
				))
				if err != nil {
					return fmt.Errorf("failed to start control server: %w", err)
				}

				shutdownFuncs = append(shutdownFuncs, shutdown)
			}

			productFeatures := productfeatures.NewClient(logger, tracerProvider, db, redisClient)

			billingRepo, billingTracker, err := newBillingProvider(ctx, logger, tracerProvider, guardianPolicy, redisClient, posthogClient, c)
			if err != nil {
				return fmt.Errorf("failed to create billing provider: %w", err)
			}

			var openRouter openrouter.Provisioner
			if c.String("environment") == "local" {
				openRouter = openrouter.NewDevelopment(c.String("openrouter-dev-key"))
			} else {
				openRouter = openrouter.New(logger, tracerProvider, guardianPolicy, db, c.String("environment"), c.String("openrouter-provisioning-key"), &background.OpenRouterKeyRefresher{TemporalEnv: temporalEnv}, productFeatures, billingTracker)
			}

			tigrisStore, shutdown, err := newTigrisStore(ctx, c, logger)
			if err != nil {
				return fmt.Errorf("failed to create tigris asset store: %w", err)
			}
			shutdownFuncs = append(shutdownFuncs, shutdown)

			functionsOrchestrator, shutdown, err := newFunctionOrchestrator(c, logger, tracerProvider, guardianPolicy, db, assetStorage, tigrisStore, encryptionClient)
			if err != nil {
				return fmt.Errorf("failed to create functions orchestrator: %w", err)
			}
			shutdownFuncs = append(shutdownFuncs, shutdown)

			runnerVersion := functions.RunnerVersion(conv.Default(strings.TrimPrefix(c.String("functions-runner-version"), "sha-"), GitSHA))

			slackClient := slack_client.NewSlackClient(guardianPolicy, "", "", db, encryptionClient)

			logsEnabled := newFeatureChecker(logger, productFeatures, productfeatures.FeatureLogs)
			toolIOLogsEnabled := newFeatureChecker(logger, productFeatures, productfeatures.FeatureToolIOLogs)
			sessionCaptureEnabled := newFeatureChecker(logger, productFeatures, productfeatures.FeatureSessionCapture)
			rbacEnabled := authz.IsRBACEnabled(newFeatureChecker(logger, productFeatures, productfeatures.FeatureRBAC))
			challengeLoggingEnabled := authz.ChallengeLoggingEnabled(newFeatureChecker(logger, productFeatures, productfeatures.FeatureAuthzChallengeLogging))

			// Create ClickHouse client and telemetry service for resolution events
			chDB, chShutdown, err := newClickhouseClient(ctx, logger, c)
			if err != nil {
				return fmt.Errorf("failed to connect to clickhouse database: %w", err)
			}
			shutdownFuncs = append(shutdownFuncs, chShutdown)

			// we don't require a real workOS client for workers as they bypass RBAC
			authzEngine := authz.NewEngine(
				logger,
				db,
				chDB,
				rbacEnabled,
				challengeLoggingEnabled,
				workos.NewStubClient(),
				cache.NewRedisCacheAdapter(redisClient),
				authz.EngineOpts{DevMode: c.String("environment") == "local"},
			)

			workosEventsClient, err := newWorkOSEventsClient(c, guardianPolicy)
			if err != nil {
				return fmt.Errorf("failed to create WorkOS events client: %w", err)
			}

			telemetryLogger, shutdown := newTelemetryLogger(ctx, logger, chDB, logsEnabled, toolIOLogsEnabled)
			shutdownFuncs = append(shutdownFuncs, shutdown)

			telemetryService := telemetry.NewService(logger, tracerProvider, db, chDB, nil, nil, logsEnabled, sessionCaptureEnabled, posthogClient, authzEngine)

			/**
			 * BEGIN -- MCP service setup for agent client
			 */

			chatWriter, chatWriterShutdown := chat.NewChatMessageWriter(logger, db, assetStorage)
			shutdownFuncs = append(shutdownFuncs, chatWriterShutdown)

			captureStrategy := chat.NewChatMessageCaptureStrategy(logger, db, chatWriter)

			riskSignaler := background.NewThrottledSignaler(
				&background.TemporalRiskAnalysisSignaler{TemporalEnv: temporalEnv, Logger: logger},
				30*time.Second,
				logger,
			)
			shutdownFuncs = append(shutdownFuncs, riskSignaler.Shutdown)
			chatWriter.AddObserver(risk.NewObserver(logger, tracerProvider, db, riskSignaler, auditLogger))

			completionsClient := openrouter.NewUnifiedClient(
				logger,
				guardianPolicy,
				openRouter,
				captureStrategy,
				chat.NewDefaultUsageTrackingStrategy(db, logger, openRouter, billingTracker, &background.FallbackModelUsageTracker{TemporalEnv: temporalEnv}),
				&background.TemporalChatTitleGenerator{TemporalEnv: temporalEnv},
				&background.TemporalDelayedChatResolutionAnalyzer{TemporalEnv: temporalEnv},
				telemetryLogger,
			)

			ragService := rag.NewToolsetVectorStore(logger, tracerProvider, db, completionsClient)
			mcpRegistryClient, err := newMCPRegistryClient(logger, tracerProvider, guardianPolicy, mcpRegistryClientOptions{
				pulseTenantID: c.String("pulse-registry-tenant"),
				pulseAPIKey:   conv.NewSecret([]byte(c.String("pulse-registry-api-key"))),
				cacheImpl:     cache.NewRedisCacheAdapter(redisClient),
			})
			if err != nil {
				return fmt.Errorf("failed to create mcp registry client: %w", err)
			}

			serverURL, err := url.Parse(c.String("server-url"))
			if err != nil {
				return fmt.Errorf("failed to parse server url: %w", err)
			}

			pylonClient, err := pylon.NewPylon(logger, c.String("pylon-verification-secret"))
			if err != nil {
				return fmt.Errorf("failed to create pylon client: %w", err)
			}

			speakeasyIDPClient := speakeasyclient.NewClient(logger, tracerProvider, guardianPolicy, c.String("speakeasy-server-address"), c.String("speakeasy-secret-key"), db, nil, posthogClient)
			sessionManager := sessions.NewManager(logger, tracerProvider, guardianPolicy, db, redisClient, cache.SuffixNone, c.String("speakeasy-server-address"), c.String("speakeasy-secret-key"), pylonClient, posthogClient, billingRepo, nil, speakeasyIDPClient)

			chatSessionsManager := chatsessions.NewManager(logger, redisClient, c.String("jwt-signing-key"))

			oauthService := oauth.NewService(logger, tracerProvider, meterProvider, db, serverURL, cache.NewRedisCacheAdapter(redisClient), encryptionClient, env, sessionManager, guardianPolicy)
			triggerApp := newTriggersApp(logger, db, encryptionClient, temporalEnv, telemetryLogger, serverURL)

			assistantTokenManager := assistanttokens.New(c.String("jwt-signing-key"), db, authzEngine)

			shadowMCPClient := shadowmcp.NewClient(logger, db, cache.NewRedisCacheAdapter(redisClient))

			mcpService := mcp.NewService(
				logger,
				tracerProvider,
				meterProvider,
				db,
				sessionManager,
				chatSessionsManager,
				env,
				posthogClient,
				serverURL,
				encryptionClient,
				cache.NewRedisCacheAdapter(redisClient),
				guardianPolicy,
				functionsOrchestrator,
				oauthService,
				billingTracker,
				billingRepo,
				telemetryLogger,
				telemetryService,
				ragService,
				triggerApp,
				temporalEnv,
				authzEngine,
				assistantTokenManager,
				shadowMCPClient,
				auditLogger,
			)

			chatClient := chat.NewAgenticChatClient(
				logger,
				db,
				env,
				cache.NewRedisCacheAdapter(redisClient),
				completionsClient,
				mcpclient.NewInternalMCPClient(mcpService),
			)

			assistantRuntime, err := newAssistantRuntime(ctx, logger, tracerProvider, c, guardianPolicy, db, serverURL)
			if err != nil {
				return err
			}
			assistantsCore := assistants.NewServiceCore(logger, tracerProvider, db, assistantRuntime, slackClient, assistantTokenManager, serverURL, telemetryLogger)
			assistantsSvc := assistants.NewService(logger, tracerProvider, db, sessionManager, authzEngine, assistantsCore, &background.AssistantWorkflowSignaler{TemporalEnv: temporalEnv})
			triggerApp.RegisterDispatcher(assistantsSvc)

			/**
			 * END -- Agent client
			 */

			var piiScanner risk_analysis.PIIScanner = &risk_analysis.StubPIIScanner{}
			if presidioURL := c.String("presidio-analyzer-url"); presidioURL != "" {
				piiScanner = risk_analysis.NewPresidioClient(presidioURL, tracerProvider, meterProvider, logger)
				logger.InfoContext(ctx, "presidio PII scanner enabled", attr.SlogURL(presidioURL))
			}

			temporalWorker := background.NewTemporalWorker(temporalEnv, logger, tracerProvider, meterProvider, &background.WorkerOptions{
				GuardianPolicy:      guardianPolicy,
				DB:                  db,
				EncryptionClient:    encryptionClient,
				FeatureProvider:     featureFlags,
				AssetStorage:        assetStorage,
				SlackClient:         slackClient,
				ChatClient:          chatClient,
				OpenRouter:          openRouter,
				K8sClient:           k8sClient,
				ExpectedTargetCNAME: c.String("custom-domain-cname"),
				BillingTracker:      billingTracker,
				BillingRepository:   billingRepo,
				RedisClient:         redisClient,
				PosthogClient:       posthogClient,
				FunctionsDeployer:   functionsOrchestrator,
				FunctionsVersion:    runnerVersion,
				RagService:          ragService,
				MCPRegistryClient:   mcpRegistryClient,
				TelemetryLogger:     telemetryLogger,
				TriggersApp:         triggerApp,
				CacheAdapter:        cache.NewRedisCacheAdapter(redisClient),
				AssistantsCore:      assistantsCore,
				TemporalEnv:         temporalEnv,
				PIIScanner:          piiScanner,
				ShadowMCPClient:     shadowMCPClient,
				AuditLogger:         auditLogger,
				WorkOSEventsClient:  workosEventsClient,
			})

			return temporalWorker.Run(worker.InterruptCh())
		},
		Before: func(ctx *cli.Context) error {
			return loadConfigFromFile(ctx, flags)
		},
		After: func(c *cli.Context) error {
			return runShutdown(PullLogger(c.Context), c.Context, shutdownFuncs)
		},
	}
}
