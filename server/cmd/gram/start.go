package gram

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/sourcegraph/conc/pool"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/rag"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.temporal.io/sdk/client"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/about"
	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/agentsapi"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatsessionssvc "github.com/speakeasy-api/gram/server/internal/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/control"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/deployments"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/instances"
	"github.com/speakeasy-api/gram/server/internal/integrations"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/keys"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oauth"
	"github.com/speakeasy-api/gram/server/internal/packages"
	"github.com/speakeasy-api/gram/server/internal/projects"
	"github.com/speakeasy-api/gram/server/internal/resources"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/templates"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/pylon"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/slack"
	slack_client "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"

	"github.com/speakeasy-api/gram/server/internal/tools"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
	"github.com/speakeasy-api/gram/server/internal/usage"
	"github.com/speakeasy-api/gram/server/internal/variations"
)

func newStartCommand() *cli.Command {
	var shutdownFuncs []func(context.Context) error

	flags := []cli.Flag{
		&cli.StringFlag{
			Name:    "address",
			Value:   ":8080",
			Usage:   "HTTP address to listen on",
			EnvVars: []string{"GRAM_SERVER_ADDRESS"},
		},
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
			Name:     "ssl-key-file",
			Usage:    "The SSL key file path to use for the server",
			Required: false,
			EnvVars:  []string{"GRAM_SSL_KEY_FILE"},
		},
		&cli.StringFlag{
			Name:     "ssl-cert-file",
			Usage:    "The SSL certifate file path to use for the server",
			Required: false,
			EnvVars:  []string{"GRAM_SSL_CERT_FILE"},
		},
		&cli.StringFlag{
			Name:    "control-address",
			Value:   ":8081",
			Usage:   "HTTP address to listen on",
			EnvVars: []string{"GRAM_CONTROL_ADDRESS"},
		},
		&cli.StringFlag{
			Name:    "unsafe-local-env-path",
			Usage:   "The path to the local environment file used for session auth in local development",
			EnvVars: []string{"GRAM_UNSAFE_LOCAL_ENV_PATH"},
		},
		&cli.StringFlag{
			Name:     "site-url",
			Usage:    "The URL of the site",
			EnvVars:  []string{"GRAM_SITE_URL"},
			Required: true,
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
			Name:    "redis-cache-addr",
			Usage:   "Address of the redis cache server",
			EnvVars: []string{"GRAM_REDIS_CACHE_ADDR"},
		},
		&cli.StringFlag{
			Name:    "redis-cache-password",
			Usage:   "Password for the redis cache server",
			EnvVars: []string{"GRAM_REDIS_CACHE_PASSWORD"},
		},
		&cli.StringFlag{
			Name:     "encryption-key",
			Usage:    "Key for App level AES encryption/decyryption",
			Required: true,
			EnvVars:  []string{"GRAM_ENCRYPTION_KEY"},
		},
		&cli.StringFlag{
			Name:     "jwt-signing-key",
			Usage:    "Key for JWT signing",
			Required: true,
			EnvVars:  []string{"GRAM_JWT_SIGNING_KEY"},
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
		&cli.StringFlag{
			Name:    "temporal-address",
			Usage:   "Address of the Temporal server",
			EnvVars: []string{"TEMPORAL_ADDRESS"},
		},
		&cli.StringFlag{
			Name:    "temporal-namespace",
			Usage:   "Namespace of the Temporal server",
			EnvVars: []string{"TEMPORAL_NAMESPACE"},
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
		&cli.BoolFlag{
			Name:    "dev-single-process",
			Usage:   "Run the server and worker in a single process for local development",
			EnvVars: []string{"GRAM_SINGLE_PROCESS"},
			Value:   false,
		},
		&cli.StringFlag{
			Name:     "slack-client-secret",
			Usage:    "The slack client secret",
			EnvVars:  []string{"SLACK_CLIENT_SECRET"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "slack-signing-secret",
			Usage:    "The slack signing secret",
			EnvVars:  []string{"SLACK_SIGNING_SECRET"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "pylon-verification-secret",
			Usage:    "The identity verification secret for pylon",
			EnvVars:  []string{"PYLON_VERIFICATION_SECRET"},
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
			Name:     "polar-api-key",
			Usage:    "The polar API key",
			EnvVars:  []string{"POLAR_API_KEY"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "polar-webhook-secret",
			Usage:    "The polar webhook secret",
			EnvVars:  []string{"POLAR_WEBHOOK_SECRET"},
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
		&cli.StringSliceFlag{
			Name:     "disallowed-cidr-blocks",
			Usage:    "List of CIDR blocks to block for SSRF protection",
			EnvVars:  []string{"GRAM_DISALLOWED_CIDR_BLOCKS"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "local-feature-flags-csv",
			Usage:    "Path to a CSV file containing local feature flags. Format: distinct_id,flag,enabled (with header row).",
			EnvVars:  []string{"GRAM_LOCAL_FEATURE_FLAGS_CSV"},
			Required: false,
		},
		&cli.StringFlag{
			Name:    "custom-domain-cname",
			Usage:   "The expected CNAME target for custom domain verification (e.g., cname.getgram.ai.)",
			EnvVars: []string{"GRAM_CUSTOM_DOMAIN_CNAME"},
		},
		&cli.PathFlag{
			Name:     "config-file",
			Usage:    "Path to a config file to load. Supported formats are JSON, TOML and YAML.",
			EnvVars:  []string{"GRAM_CONFIG_FILE"},
			Required: false,
		},
	}

	flags = append(flags, clickHouseFlags...)
	flags = append(flags, functionsFlags...)
	flags = append(flags, pulseMCPFlags...)

	return &cli.Command{
		Name:  "start",
		Usage: "Start the Gram API server",
		Flags: flags,
		Action: func(c *cli.Context) error {
			serviceName := "gram-server"
			serviceEnv := c.String("environment")
			appinfo := o11y.PullAppInfo(c.Context)
			appinfo.Command = "server"
			logger := PullLogger(c.Context).With(
				attr.SlogServiceName(serviceName),
				attr.SlogServiceVersion(shortGitSHA()),
				attr.SlogServiceEnv(serviceEnv),
			)
			tracerProvider := otel.GetTracerProvider()
			meterProvider := otel.GetMeterProvider()

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
				return fmt.Errorf("setup opentelemetry sdk: %w", err)
			}
			shutdownFuncs = append(shutdownFuncs, shutdown)

			db, err := newDBClient(ctx, logger, meterProvider, c.String("database-url"), dbClientOptions{
				enableUnsafeLogging: c.Bool("unsafe-db-log"),
			})
			if err != nil {
				return fmt.Errorf("failed to connect to database: %w", err)
			}
			// Ping the database to ensure connectivity
			if err := db.Ping(ctx); err != nil {
				logger.ErrorContext(ctx, "failed to ping database", attr.SlogError(err))
				return fmt.Errorf("database ping failed: %w", err)
			}
			defer db.Close()

			err = o11y.StartObservers(meterProvider, db)
			if err != nil {
				return fmt.Errorf("failed to create observers: %w", err)
			}

			assetStorage, shutdown, err := newAssetStorage(ctx, logger, assetStorageOptions{
				assetsBackend: c.String("assets-backend"),
				assetsURI:     c.String("assets-uri"),
			})
			if err != nil {
				return fmt.Errorf("failed to initialize asset storage: %w", err)
			}
			shutdownFuncs = append(shutdownFuncs, shutdown)

			redisClient, err := newRedisClient(ctx, redisClientOptions{
				redisAddr:     c.String("redis-cache-addr"),
				redisPassword: c.String("redis-cache-password"),
			})
			if err != nil {
				return fmt.Errorf("failed to connect to redis: %w", err)
			}

			pylonClient, err := pylon.NewPylon(logger, c.String("pylon-verification-secret"))
			if err != nil {
				return fmt.Errorf("failed to create pylon client: %w", err)
			}

			posthogClient := posthog.New(ctx, logger, c.String("posthog-api-key"), c.String("posthog-endpoint"), c.String("posthog-personal-api-key"))
			var featureFlags feature.Provider = posthogClient
			if c.String("environment") == "local" {
				featureFlags = newLocalFeatureFlags(ctx, logger, c.String("local-feature-flags-csv"))
			}

			billingRepo, billingTracker, err := newBillingProvider(ctx, logger, tracerProvider, redisClient, posthogClient, c)
			if err != nil {
				return fmt.Errorf("failed to create billing provider: %w", err)
			}

			localEnvPath := c.String("unsafe-local-env-path")
			var sessionManager *sessions.Manager
			if localEnvPath == "" {
				sessionManager = sessions.NewManager(logger, db, redisClient, cache.SuffixNone, c.String("speakeasy-server-address"), c.String("speakeasy-secret-key"), pylonClient, posthogClient, billingRepo)
			} else {
				logger.WarnContext(ctx, "enabling unsafe session store", attr.SlogFilePath(localEnvPath))
				s, err := sessions.NewUnsafeManager(logger, db, redisClient, cache.Suffix("gram-local"), localEnvPath, billingRepo)
				if err != nil {
					return fmt.Errorf("failed to create unsafe session manager: %w", err)
				}

				sessionManager = s
			}

			chatSessionsManager := chatsessions.NewManager(logger, redisClient, c.String("jwt-signing-key"))

			encryptionClient, err := encryption.New(c.String("encryption-key"))
			if err != nil {
				return fmt.Errorf("failed to create encryption client: %w", err)
			}

			env := environments.NewEnvironmentEntries(logger, db, encryptionClient)

			k8sClient, err := k8s.InitializeK8sClient(ctx, logger, c.String("environment"))
			if err != nil {
				return fmt.Errorf("failed to create kubernetes client: %w", err)
			}

			temporalClient, shutdown, err := newTemporalClient(logger, temporalClientOptions{
				address:      c.String("temporal-address"),
				namespace:    c.String("temporal-namespace"),
				certPEMBlock: []byte(c.String("temporal-client-cert")),
				keyPEMBlock:  []byte(c.String("temporal-client-key")),
			})
			if err != nil {
				return fmt.Errorf("failed to create temporal client: %w", err)
			}

			if temporalClient == nil {
				logger.WarnContext(ctx, "temporal disabled")
			} else {
				shutdownFuncs = append(shutdownFuncs, shutdown)
			}

			productFeatures := productfeatures.NewClient(logger, db, redisClient)

			var openRouter openrouter.Provisioner
			if c.String("environment") == "local" {
				openRouter = openrouter.NewDevelopment(c.String("openrouter-dev-key"))
			} else {
				openRouter = openrouter.New(logger, db, c.String("environment"), c.String("openrouter-provisioning-key"), &background.OpenRouterKeyRefresher{Temporal: temporalClient}, productFeatures, billingTracker)
			}

			{
				controlServer := control.Server{
					Address:          c.String("control-address"),
					Logger:           logger.With(attr.SlogComponent("control")),
					DisableProfiling: false,
				}

				temporals := []*o11y.NamedResource[client.Client]{}
				if temporalClient != nil {
					temporals = append(temporals, &o11y.NamedResource[client.Client]{Name: "default", Resource: temporalClient})
				}

				shutdown, err := controlServer.Start(c.Context, o11y.NewHealthCheckHandler(
					[]*o11y.NamedResource[*pgxpool.Pool]{{Name: "default", Resource: db}},
					[]*o11y.NamedResource[*redis.Client]{{Name: "default", Resource: redisClient}},
					temporals,
				))
				if err != nil {
					return fmt.Errorf("failed to start control server: %w", err)
				}

				shutdownFuncs = append(shutdownFuncs, shutdown)
			}

			serverURL, err := url.Parse(c.String("server-url"))
			if err != nil {
				return fmt.Errorf("failed to parse server url: %w", err)
			}

			siteURL, err := url.Parse(c.String("site-url"))
			if err != nil {
				return fmt.Errorf("failed to parse site url: %w", err)
			}

			guardianPolicy := guardian.NewDefaultPolicy()
			blockedCIDRs := c.StringSlice("disallowed-cidr-blocks")
			if blockedCIDRs != nil {
				guardianPolicy, err = guardian.NewUnsafePolicy(blockedCIDRs)
				if err != nil {
					return fmt.Errorf("failed to create unsafe http guardian policy: %w", err)
				}
			}

			tigrisStore, shutdown, err := newTigrisStore(ctx, c, logger)
			if err != nil {
				return fmt.Errorf("failed to create tigris asset store: %w", err)
			}
			shutdownFuncs = append(shutdownFuncs, shutdown)

			functionsOrchestrator, shutdown, err := newFunctionOrchestrator(c, logger, tracerProvider, db, assetStorage, tigrisStore, encryptionClient)
			if err != nil {
				return fmt.Errorf("failed to create functions orchestrator: %w", err)
			}
			shutdownFuncs = append(shutdownFuncs, shutdown)
			runnerVersion := functions.RunnerVersion(conv.Default(strings.TrimPrefix(c.String("functions-runner-version"), "sha-"), GitSHA))

			slackClient := slack_client.NewSlackClient(slack.SlackClientID(c.String("environment")), c.String("slack-client-secret"), db, encryptionClient)
			baseChatClient := openrouter.NewChatClient(logger, openRouter)

			tcm, shutdown, err := newToolMetricsClient(ctx, logger, c, tracerProvider, productFeatures)
			if err != nil {
				return fmt.Errorf("failed to connect to tool metrics client: %w", err)
			}
			shutdownFuncs = append(shutdownFuncs, shutdown)

			chatClient := chat.NewChatClient(logger, tracerProvider, meterProvider, db, openRouter, baseChatClient, env, encryptionClient, cache.NewRedisCacheAdapter(redisClient), guardianPolicy, functionsOrchestrator)
			ragService := rag.NewToolsetVectorStore(logger, tracerProvider, db, baseChatClient)
			mcpRegistryClient, err := newMCPRegistryClient(logger, tracerProvider, mcpRegistryClientOptions{
				pulseTenantID: c.String("pulse-registry-tenant"),
				pulseAPIKey:   conv.NewSecret([]byte(c.String("pulse-registry-api-key"))),
			})
			if err != nil {
				return fmt.Errorf("failed to create mcp registry client: %w", err)
			}

			mux := goahttp.NewMuxer()
			mux.Use(middleware.CORSMiddleware(c.String("environment"), c.String("server-url"), chatSessionsManager))
			mux.Use(middleware.NewHTTPLoggingMiddleware(logger))
			mux.Use(customdomains.Middleware(logger, db, c.String("environment"), serverURL))
			mux.Use(middleware.SessionMiddleware)
			mux.Use(middleware.AdminOverrideMiddleware)

			toolsetsSvc := toolsets.NewService(logger, db, sessionManager, cache.NewRedisCacheAdapter(redisClient))
			authAuth := auth.New(logger, db, sessionManager)

			about.Attach(mux, about.NewService(logger, tracerProvider))
			agentsapi.Attach(mux, agentsapi.NewService(logger, tracerProvider, meterProvider, db, env, encryptionClient, cache.NewRedisCacheAdapter(redisClient), guardianPolicy, functionsOrchestrator, openRouter, baseChatClient, authAuth, temporalClient, c.String("temporal-namespace")))
			auth.Attach(mux, auth.NewService(logger, db, sessionManager, auth.AuthConfigurations{
				SpeakeasyServerAddress: c.String("speakeasy-server-address"),
				GramServerURL:          c.String("server-url"),
				SignInRedirectURL:      auth.FormSignInRedirectURL(c.String("site-url")),
				Environment:            c.String("environment"),
			}))
			projects.Attach(mux, projects.NewService(logger, db, sessionManager))
			packages.Attach(mux, packages.NewService(logger, db, sessionManager))
			productfeatures.Attach(mux, productfeatures.NewService(logger, db, sessionManager, redisClient))
			toolsets.Attach(mux, toolsetsSvc)
			integrations.Attach(mux, integrations.NewService(logger, db, sessionManager))
			templates.Attach(mux, templates.NewService(logger, db, sessionManager, toolsetsSvc))
			assets.Attach(mux, assets.NewService(logger, db, sessionManager, chatSessionsManager, assetStorage, c.String("jwt-signing-key")))
			deployments.Attach(mux, deployments.NewService(logger, tracerProvider, db, temporalClient, sessionManager, assetStorage, posthogClient, siteURL, mcpRegistryClient))
			keys.Attach(mux, keys.NewService(logger, db, sessionManager, c.String("environment")))
			chatsessionssvc.Attach(mux, chatsessionssvc.NewService(logger, db, sessionManager, chatSessionsManager))
			environments.Attach(mux, environments.NewService(logger, db, sessionManager, encryptionClient))
			tools.Attach(mux, tools.NewService(logger, db, sessionManager))
			resources.Attach(mux, resources.NewService(logger, db, sessionManager))
			oauthService := oauth.NewService(logger, tracerProvider, meterProvider, db, serverURL, cache.NewRedisCacheAdapter(redisClient), encryptionClient, env, sessionManager)
			oauth.Attach(mux, oauthService)
			instances.Attach(mux, instances.NewService(logger, tracerProvider, meterProvider, db, sessionManager, chatSessionsManager, env, encryptionClient, cache.NewRedisCacheAdapter(redisClient), guardianPolicy, functionsOrchestrator, billingTracker, tcm, productFeatures, serverURL))
			mcpMetadataService := mcpmetadata.NewService(logger, db, sessionManager, serverURL, siteURL, cache.NewRedisCacheAdapter(redisClient))
			mcpmetadata.Attach(mux, mcpMetadataService)
			externalmcp.Attach(mux, externalmcp.NewService(logger, tracerProvider, db, sessionManager, mcpRegistryClient))
			mcp.Attach(mux, mcp.NewService(logger, tracerProvider, meterProvider, db, sessionManager, chatSessionsManager, env, posthogClient, serverURL, encryptionClient, cache.NewRedisCacheAdapter(redisClient), guardianPolicy, functionsOrchestrator, oauthService, billingTracker, billingRepo, tcm, productFeatures, ragService, temporalClient), mcpMetadataService)
			chat.Attach(mux, chat.NewService(logger, db, sessionManager, chatSessionsManager, openRouter, baseChatClient, &background.FallbackModelUsageTracker{Temporal: temporalClient}, posthogClient))
			if slackClient.Enabled() {
				slack.Attach(mux, slack.NewService(logger, db, sessionManager, encryptionClient, redisClient, slackClient, temporalClient, slack.Configurations{
					GramServerURL:      c.String("server-url"),
					SignInRedirectURL:  auth.FormSignInRedirectURL(c.String("site-url")),
					SlackAppInstallURL: slack.SlackInstallURL(c.String("environment")),
					SlackSigningSecret: c.String("slack-signing-secret"),
				}))
			}
			variations.Attach(mux, variations.NewService(logger, db, sessionManager))
			customdomains.Attach(mux, customdomains.NewService(logger, db, sessionManager, &background.CustomDomainRegistrationClient{Temporal: temporalClient}))
			usage.Attach(mux, usage.NewService(logger, db, sessionManager, billingRepo, serverURL, posthogClient, openRouter))
			tm.Attach(mux, tm.NewService(logger, db, sessionManager, chatSessionsManager, tcm, productFeatures, posthogClient))
			functions.Attach(mux, functions.NewService(logger, tracerProvider, db, encryptionClient, tigrisStore))

			srv := &http.Server{
				Addr:              c.String("address"),
				Handler:           otelhttp.NewHandler(mux, "http", otelhttp.WithServerName("gram")),
				ReadHeaderTimeout: 10 * time.Second,
				BaseContext: func(net.Listener) context.Context {
					return ctx
				},
			}

			sigctx, sigcancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
			defer sigcancel()

			group := pool.New()

			if temporalClient != nil && c.Bool("dev-single-process") {
				// Create agents service for the worker
				agentsWorkerSvc := agents.NewService(logger, tracerProvider, meterProvider, db, env, encryptionClient, cache.NewRedisCacheAdapter(redisClient), guardianPolicy, functionsOrchestrator, openRouter, baseChatClient)

				workerInterruptCh := make(chan any)
				group.Go(func() {
					<-sigctx.Done()
					close(workerInterruptCh)
				})
				group.Go(func() {
					temporalWorker := background.NewTemporalWorker(temporalClient, logger, tracerProvider, meterProvider, &background.WorkerOptions{
						DB:                   db,
						EncryptionClient:     encryptionClient,
						FeatureProvider:      featureFlags,
						AssetStorage:         assetStorage,
						SlackClient:          slackClient,
						ChatClient:           chatClient,
						OpenRouterChatClient: baseChatClient,
						OpenRouter:           openRouter,
						K8sClient:            k8sClient,
						ExpectedTargetCNAME:  c.String("custom-domain-cname"),
						BillingTracker:       billingTracker,
						BillingRepository:    billingRepo,
						RedisClient:          redisClient,
						PosthogClient:        posthogClient,
						FunctionsDeployer:    functionsOrchestrator,
						FunctionsVersion:     runnerVersion,
						RagService:           ragService,
						AgentsService:        agentsWorkerSvc,
						MCPRegistryClient:    mcpRegistryClient,
					})
					if err := temporalWorker.Run(workerInterruptCh); err != nil {
						logger.ErrorContext(ctx, "temporal worker failed", attr.SlogError(err))
					}
				})
			}

			group.Go(func() {
				<-sigctx.Done()

				logger.InfoContext(ctx, "shutting down server")

				graceCtx, graceCancel := context.WithTimeout(ctx, 10*time.Second)
				defer graceCancel()

				if err := srv.Shutdown(graceCtx); err != nil {
					logger.ErrorContext(ctx, "failed to shutdown development server", attr.SlogError(err))
				}
			})

			tlsEnabled := c.String("ssl-key-file") != "" && c.String("ssl-cert-file") != ""
			if tlsEnabled {
				logger.InfoContext(ctx, "server started with tls", attr.SlogServerAddress(c.String("address")))
				if err := srv.ListenAndServeTLS(c.String("ssl-cert-file"), c.String("ssl-key-file")); err != nil && !errors.Is(err, http.ErrServerClosed) {
					logger.ErrorContext(ctx, "server error", attr.SlogError(err))
				}
			} else {
				logger.InfoContext(ctx, "server started", attr.SlogServerAddress(c.String("address")))
				if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					logger.ErrorContext(ctx, "server error", attr.SlogError(err))
				}
			}

			cancel()
			group.Wait()

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
