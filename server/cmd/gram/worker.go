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

	"github.com/speakeasy-api/gram/server/internal/accesscontrol"
	"github.com/speakeasy-api/gram/server/internal/assistants"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/control"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/email"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpclient"
	mcpmetadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/memory"
	"github.com/speakeasy-api/gram/server/internal/modelkeys"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oauth"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	platformtoolsruntime "github.com/speakeasy-api/gram/server/internal/platformtools/runtime"
	platformskills "github.com/speakeasy-api/gram/server/internal/platformtools/skills"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/rag"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/presetlib"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/customruleanalyzer"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	piopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptinjection/openrouter"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/skills/efficacy"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	ghclient "github.com/speakeasy-api/gram/server/internal/thirdparty/github"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/loops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/pylon"
	slack_client "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	userRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/speakeasy-api/gram/tunnel/route"
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
		&cli.BoolFlag{
			Name:    "enable-gateway-ip-allowlist",
			Usage:   "Enable Envoy Gateway SecurityPolicy reconcile for custom domain IP allow listing. Requires the SecurityPolicy CRD to be installed.",
			EnvVars: []string{"GRAM_ENABLE_GATEWAY_IP_ALLOWLIST"},
			Value:   false,
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
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:     "polar-product-id-assistants",
			Aliases:  []string{"polar.product_id_assistants"},
			Usage:    "The product ID granting the assistants benefit in Polar (auto-attached on assistants-disposition signup)",
			EnvVars:  []string{"POLAR_PRODUCT_ID_ASSISTANTS"},
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
			Name:    "custom-domain-provisioner",
			Usage:   "Kubernetes provisioner kind for custom domains: ingress or gateway (default: ingress)",
			EnvVars: []string{"GRAM_CUSTOM_DOMAIN_PROVISIONER"},
			Value:   "ingress",
		},
		&cli.StringFlag{
			Name:     "pylon-verification-secret",
			Usage:    "The identity verification secret for pylon",
			EnvVars:  []string{"PYLON_VERIFICATION_SECRET"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "idp-base-url",
			Usage:    "OIDC identity provider base URL (e.g. http://localhost:35291/oauth2)",
			EnvVars:  []string{"GRAM_IDP_BASE_URL"},
			Required: true,
		},
		&cli.StringFlag{
			Name:     "idp-client-id",
			Usage:    "OIDC client ID for the identity provider",
			EnvVars:  []string{"GRAM_IDP_CLIENT_ID"},
			Required: true,
		},
		&cli.StringFlag{
			Name:    "idp-client-secret",
			Usage:   "WorkOS API key for user management and identity lookups",
			EnvVars: []string{"GRAM_IDP_CLIENT_SECRET"},
		},
		&cli.StringFlag{
			Name:     usersessions.JWTSigningKeyFlag,
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
			Name:     "workos-endpoint",
			Usage:    "Base URL for WorkOS API calls. Leave unset for production (defaults to https://api.workos.com); set to the dev-idp's mock-workos mode for fully-local development.",
			EnvVars:  []string{"WORKOS_API_URL"},
			Required: false,
		},
		&cli.StringFlag{
			Name:    "presidio-analyzer-url",
			Usage:   "Base URL of the Presidio Analyzer service (e.g. http://presidio-analyzer:3000). Empty disables PII scanning.",
			EnvVars: []string{"PRESIDIO_ANALYZER_URL"},
		},
		&cli.StringFlag{
			Name:     "loops-api-key",
			Usage:    "Loops API key for transactional emails (billing usage alerts). Empty or 'unset' disables email sending.",
			EnvVars:  []string{"LOOPS_API_KEY"},
			Required: false,
		},
	}

	flags = append(flags, redisFlags...)
	flags = append(flags, clickHouseFlags...)
	flags = append(flags, functionsFlags...)
	flags = append(flags, pulseMCPFlags...)
	flags = append(flags, assistantRuntimeFlags...)
	flags = append(flags, svixFlags...)
	flags = append(flags, pluginsFlags...)
	flags = append(flags, posthogFlags...)
	flags = append(flags, gcpFlags...)

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
			slog.SetDefault(logger)

			if serviceEnv == "local" {
				scanners.EnableRuleIDFormatEnforcement()
			}

			ctx, cancel := context.WithCancel(c.Context)
			defer cancel()

			shutdown, err := o11y.SetupOTelSDK(ctx, logger, o11y.SetupOTelSDKOptions{
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

			tracerProvider := otel.GetTracerProvider()
			meterProvider := otel.GetMeterProvider()

			temporalEnv, shutdown, err := newTemporalClient(logger, meterProvider, temporalClientOptions{
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

			db, err := newDBClient(ctx, logger, meterProvider, c.String("database-url"), dbClientOptions{
				enableUnsafeLogging: c.Bool("unsafe-db-log"),
			})
			if err != nil {
				return err
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

			guardianPolicy, err := newGuardianPolicy(c, logger, tracerProvider, meterProvider, redisClient)
			if err != nil {
				return err
			}

			encryptionClient, err := encryption.New(c.String("encryption-key"))
			if err != nil {
				return fmt.Errorf("failed to create encryption client: %w", err)
			}

			auditLogger := newAuditLogger()

			var ghClient *ghclient.Client
			if appID, key := c.Int64("plugins-github-app-id"), c.String("plugins-github-private-key"); appID != 0 && key != "" {
				ghClient, err = ghclient.NewClient(appID, []byte(key), guardianPolicy.Client())
				if err != nil {
					return fmt.Errorf("create github app client: %w", err)
				}
			}
			pluginsGitHub, err := plugins.NewGitHubConfig(plugins.GitHubConfigInput{
				Client:         ghClient,
				Org:            c.String("plugins-github-org"),
				InstallationID: c.Int64("plugins-github-installation-id"),
			})
			if err != nil {
				return fmt.Errorf("plugins github config: %w", err)
			}
			posthogClient := posthog.New(ctx, logger, c.String("posthog-api-key"), c.String("posthog-endpoint"), c.String("posthog-personal-api-key"))
			var featureFlags feature.Provider = posthogClient
			if c.String("environment") == "local" {
				featureFlags = newLocalFeatureFlags(ctx, logger, c.String("local-feature-flags-csv"))
			}

			var pluginPublisher *plugins.Service
			if pluginsGitHub != nil {
				logger.InfoContext(ctx, "GitHub publishing for plugins: enabled")
				pluginPublisher = plugins.NewPublisher(logger, db, auditLogger, pluginsGitHub, c.String("environment"), c.String("server-url"), featureFlags)
			} else {
				logger.InfoContext(ctx, "GitHub publishing for plugins: disabled")
			}

			mcpMetadataRepo := mcpmetadata_repo.New(db)
			env := environments.NewEnvironmentEntries(logger, db, encryptionClient, mcpMetadataRepo)

			k8sClient, err := k8s.InitializeK8sClient(ctx, logger, c.String("environment"), c.Bool("enable-gateway-ip-allowlist"))
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

			loopsClient := loops.New(ctx, logger, guardianPolicy, c.String("loops-api-key"))
			emailService := email.NewService(logger, loopsClient)

			_, psbroker, pubsubShutdown, err := newPubSubClient(ctx, c, logger)
			if err != nil {
				shutdownFuncs = append(shutdownFuncs, pubsubShutdown)
				return fmt.Errorf("failed to create pubsub client: %w", err)
			}

			publishers, shutdown, err := newPublishers(ctx, psbroker)
			// Make sure topics are stopped and flushed before the pubsub client
			// is stopped.
			shutdownFuncs = append(shutdownFuncs, shutdown, pubsubShutdown)
			if err != nil {
				return fmt.Errorf("failed to create publishers: %w", err)
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

			svixClient, shutdown, err := newSvixClient(c, logger, guardianPolicy)
			if shutdown != nil {
				shutdownFuncs = append(shutdownFuncs, shutdown)
			}
			if err != nil {
				return fmt.Errorf("failed to create svix webhook sender: %w", err)
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

			slackClient := slack_client.NewSlackClient(guardianPolicy)

			logsEnabled := newFeatureChecker(logger, productFeatures, productfeatures.FeatureLogs)
			toolIOLogsEnabled := newFeatureChecker(logger, productFeatures, productfeatures.FeatureToolIOLogs)
			sessionCaptureEnabled := newFeatureChecker(logger, productFeatures, productfeatures.FeatureSessionCapture)
			rbacEnabled := authz.IsRBACEnabled(newFeatureChecker(logger, productFeatures, productfeatures.FeatureRBAC))
			challengeLoggingEnabled := authz.ChallengeLoggingEnabled(newFeatureChecker(logger, productFeatures, productfeatures.FeatureAuthzChallengeLogging))

			// Create ClickHouse client and telemetry service for resolution events
			chDB, chShutdown, err := newClickhouseClient(ctx, logger, tracerProvider, meterProvider, c)
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
				authz.EngineOpts{DevMode: c.String("environment") == "local"},
			)

			workosClient, workosAvailable, err := newWorkOSClient(guardianPolicy, c)
			if err != nil {
				return fmt.Errorf("failed to create WorkOS client: %w", err)
			}
			var backgroundWorkOSClient activities.WorkOSClient = workosClient
			if !workosAvailable {
				backgroundWorkOSClient = workos.NewStubClient()
			}

			telemetryLogger, shutdown := newTelemetryLogger(ctx, logger, tracerProvider, meterProvider, db, cache.NewRedisCacheAdapter(redisClient), chDB, logsEnabled, toolIOLogsEnabled)
			shutdownFuncs = append(shutdownFuncs, shutdown)

			telemetryService := telemetry.NewService(logger, tracerProvider, db, chDB, nil, nil, logsEnabled, sessionCaptureEnabled, posthogClient, authzEngine)

			/**
			 * BEGIN -- MCP service setup for agent client
			 */

			chatWriter, chatWriterShutdown := chat.NewChatMessageWriter(logger, db, assetStorage)
			shutdownFuncs = append(shutdownFuncs, chatWriterShutdown)

			captureStrategy := chat.NewChatMessageCaptureStrategy(logger, meterProvider, db, chatWriter)

			riskSignaler := background.NewThrottledSignaler(
				&background.TemporalRiskAnalysisSignaler{TemporalEnv: temporalEnv, Logger: logger},
				30*time.Second,
				logger,
			)
			// riskSignaler.Shutdown is flushed synchronously after temporalWorker.Run
			// returns (below), not via shutdownFuncs, to avoid racing the concurrent
			// temporalClient.Close() over the same gRPC connection.
			chatWriter.AddObserver(risk.NewObserver(logger, tracerProvider, db, riskSignaler, auditLogger))

			// Throttled for the same reason riskSignaler is: the writer emits one
			// wake per durable message write and a wake carries no payload, so a
			// burst of them coalesces into the single pass they all ask for. Its
			// flush shares the deferred drain below.
			efficacySignaler := background.NewThrottledSignaler(
				&background.TemporalSkillEfficacySignaler{TemporalEnv: temporalEnv, Logger: logger},
				background.SkillEfficacySignalCooldown,
				logger.With(attr.SlogComponent("skill-efficacy")),
			)
			chatWriter.AddObserver(efficacy.NewObserver(logger, efficacySignaler))

			completionsClient := openrouter.NewUnifiedClient(
				logger,
				guardianPolicy,
				openRouter,
				modelkeys.NewResolver(db, encryptionClient, openRouter),
				captureStrategy,
				chat.NewDefaultUsageTrackingStrategy(db, logger, billingTracker),
				&background.TemporalChatTitleGenerator{TemporalEnv: temporalEnv},
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

			idpClientSecret := c.String("idp-client-secret")

			umClient := newIDPUserManagementClient(guardianPolicy, idpClientSecret, c)
			if umClient == nil {
				return fmt.Errorf("failed to create IDP user management client: idp-client-secret is required")
			}

			idpClient := identity.NewWorkOSAdapter(umClient)

			identityResolver := identity.NewResolver(
				logger,
				tracerProvider,
				cache.NewRedisCacheAdapter(redisClient),
				c.String("idp-base-url"),
				c.String("idp-client-id"),
				idpClient,
				nil, // no WorkOS client in worker
				orgRepo.New(db),
				userRepo.New(db),
				pylonClient,
				posthogClient,
				productFeatures,
				cache.SuffixNone,
			)

			sessionManager := sessions.NewManager(logger, tracerProvider, db, redisClient, cache.SuffixNone, idpClient, billingRepo, identityResolver)

			chatSessionsManager := chatsessions.NewManager(logger, redisClient, c.String(usersessions.JWTSigningKeyFlag))

			oauthService := oauth.NewService(logger, tracerProvider, meterProvider, db, serverURL, cache.NewRedisCacheAdapter(redisClient), encryptionClient, env, sessionManager, identityResolver, guardianPolicy)
			// The worker never serves webhook ingress (ProcessWebhook lives in
			// the HTTP server), so the dashboard site URL used for Slack link
			// unfurls is not needed here.
			triggerApp := newTriggersApp(logger, db, encryptionClient, temporalEnv, telemetryLogger, auditLogger, serverURL, nil, slackClient)

			assistantTokenManager := assistanttokens.New(c.String(usersessions.JWTSigningKeyFlag), db, authzEngine)

			accessStore := accesscontrol.NewRedisStore(cache.NewRedisCacheAdapter(redisClient), accesscontrol.AlphaTTL)
			shadowMCPClient := shadowmcp.NewClient(logger, db, cache.NewRedisCacheAdapter(redisClient), accessStore, serverURL)

			memorySvc := memory.NewMemoryService(
				logger,
				tracerProvider,
				meterProvider,
				db,
				completionsClient,
				auditLogger,
			)
			memoryTools := platformtoolsruntime.MemoryExternalTools(memorySvc)
			skillTools := platformtoolsruntime.AssistantSkillTools(logger, db, platformskills.WithEfficacySignaler(efficacySignaler))
			// Runner-callable platform tools the runtime must be able to execute.
			assistantPlatformExtras := append([]platformtools.ExternalTool{}, memoryTools...)
			assistantPlatformExtras = append(assistantPlatformExtras, skillTools...)
			platformFeatureChecker := productFeatures.PlatformFeatureCheck

			remoteChallengeManager := remotesessions.NewChallengeManager(
				logger,
				db,
				encryptionClient,
				guardianPolicy,
				cache.NewRedisCacheAdapter(redisClient),
				serverURL,
			)

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
				assistantPlatformExtras,
				platformFeatureChecker,
				nil,
				identityResolver,
				usersessions.NewSigner(c.String(usersessions.JWTSigningKeyFlag)),
				remoteChallengeManager,
				// remoteProxyManager is HTTP-only; the worker never serves a
				// runtime request through mcp.Service, so the factory is
				// intentionally nil here.
				nil,
				route.NewRouteTable(),
				"",
				nil,
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
			contextWindowResolver := openrouter.NewContextWindowResolver(logger, guardianPolicy, cache.NewRedisCacheAdapter(redisClient))
			assistantsCore := assistants.NewServiceCore(logger, tracerProvider, meterProvider, db, guardianPolicy, encryptionClient, assistantRuntime, slackClient, assistantTokenManager, serverURL, telemetryLogger, contextWindowResolver, auditLogger)
			assistantsCore.SetWakeCanceller(triggerApp)
			assistantsCore.SetDashboardIngestor(triggerApp)
			assistantsCore.SetChatMessageWriter(chatWriter)
			assistantsSvc := assistants.NewService(logger, tracerProvider, meterProvider, db, sessionManager, authzEngine, assistantsCore, &background.AssistantWorkflowSignaler{TemporalEnv: temporalEnv}, ratelimit.NewRedisStore(redisClient))
			triggerApp.RegisterDispatcher(assistantsSvc)

			/**
			 * END -- Agent client
			 */

			var piiScanner risk_analysis.PIIScanner = &risk_analysis.StubPIIScanner{}
			if presidioURL := c.String("presidio-analyzer-url"); presidioURL != "" {
				piiScanner = risk_analysis.NewPresidioClient(presidioURL, tracerProvider, meterProvider, logger)
				logger.InfoContext(ctx, "presidio PII scanner enabled", attr.SlogURL(presidioURL))
			}

			piScanner := promptinjection.NewScanner(logger, piopenrouter.New(logger, tracerProvider, meterProvider, completionsClient, openrouter.NewJudgeRateLimiter(ratelimit.NewRedisStore(redisClient))).Classify)

			customRuleScanner, err := customruleanalyzer.NewScanner(db)
			if err != nil {
				return fmt.Errorf("create custom rules scanner: %w", err)
			}

			builtinPresets, err := presetlib.New()
			if err != nil {
				return fmt.Errorf("load built-in exclusion library: %w", err)
			}

			temporalWorker := background.NewTemporalWorker(temporalEnv, logger, tracerProvider, meterProvider, &background.WorkerOptions{
				GuardianPolicy:                 guardianPolicy,
				DB:                             db,
				EncryptionClient:               encryptionClient,
				FeatureProvider:                featureFlags,
				AssetStorage:                   assetStorage,
				SlackClient:                    slackClient,
				ChatMessageWriter:              chatWriter,
				ChatClient:                     chatClient,
				OpenRouter:                     openRouter,
				K8sClient:                      k8sClient,
				DefaultCustomDomainProvisioner: k8s.ProvisionerKind(c.String("custom-domain-provisioner")),
				ExpectedTargetCNAME:            c.String("custom-domain-cname"),
				BillingTracker:                 billingTracker,
				BillingRepository:              billingRepo,
				RedisClient:                    redisClient,
				PosthogClient:                  posthogClient,
				EmailService:                   emailService,
				FunctionsDeployer:              functionsOrchestrator,
				FunctionsVersion:               runnerVersion,
				RagService:                     ragService,
				MCPRegistryClient:              mcpRegistryClient,
				TelemetryLogger:                telemetryLogger,
				ClickhouseConn:                 chDB,
				TelemetryRepo:                  telemetryrepo.New(chDB),
				TriggersApp:                    triggerApp,
				CacheAdapter:                   cache.NewRedisCacheAdapter(redisClient),
				AssistantsCore:                 assistantsCore,
				TemporalEnv:                    temporalEnv,
				PIIScanner:                     piiScanner,
				PIScanner:                      piScanner,
				CustomRuleScanner:              customRuleScanner,
				BuiltinPresets:                 builtinPresets,
				ShadowMCPClient:                shadowMCPClient,
				AuditLogger:                    auditLogger,
				WorkOSClient:                   backgroundWorkOSClient,
				SvixClient:                     svixClient,
				ProductFeatures:                productFeatures,
				PluginPublisher:                pluginPublisher,
				Publishers:                     publishers,
			})

			// Flush the throttle's queued trailing risk signals before this Action
			// returns, while the Temporal client is still open. The cli After hook runs
			// runShutdown, which closes the client concurrently across shutdownFuncs and
			// would otherwise race the flush ("grpc: the client connection is closing").
			// riskSignaler.Shutdown is deliberately not registered as a shutdownFunc.
			defer func() {
				if ferr := riskSignaler.Shutdown(ctx); ferr != nil {
					logger.ErrorContext(ctx, "flush pending risk signals", attr.SlogError(ferr))
				}
				if ferr := efficacySignaler.Shutdown(ctx); ferr != nil {
					logger.ErrorContext(ctx, "flush pending skill efficacy signals", attr.SlogError(ferr))
				}
			}()

			if err := temporalWorker.Run(worker.InterruptCh()); err != nil {
				return fmt.Errorf("run temporal worker: %w", err)
			}

			return nil
		},
		Before: func(ctx *cli.Context) error {
			return loadConfigFromFile(ctx, flags)
		},
		After: func(c *cli.Context) error {
			return runShutdown(PullLogger(c.Context), c.Context, shutdownFuncs)
		},
	}
}
