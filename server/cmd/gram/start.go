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
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/sourcegraph/conc/pool"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.temporal.io/sdk/client"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/auditapi"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/rag"

	"github.com/speakeasy-api/gram/server/internal/about"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/assistants"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/auth/speakeasyclient"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/background"
	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatsessionssvc "github.com/speakeasy-api/gram/server/internal/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/collections"
	"github.com/speakeasy-api/gram/server/internal/control"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/deployments"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/hooks"
	"github.com/speakeasy-api/gram/server/internal/instances"
	"github.com/speakeasy-api/gram/server/internal/integrations"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/keys"
	"github.com/speakeasy-api/gram/server/internal/marketplace"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpclient"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata"
	mcpmetadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oauth"
	"github.com/speakeasy-api/gram/server/internal/organizations"
	"github.com/speakeasy-api/gram/server/internal/packages"
	platformtoolsruntime "github.com/speakeasy-api/gram/server/internal/platformtools/runtime"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/projects"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/resources"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/templates"
	ghclient "github.com/speakeasy-api/gram/server/internal/thirdparty/github"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/pylon"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/slack"
	slack_client "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"
	"github.com/speakeasy-api/gram/server/internal/triggers"

	"github.com/speakeasy-api/gram/server/internal/tools"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
	"github.com/speakeasy-api/gram/server/internal/usage"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/speakeasy-api/gram/server/internal/variations"
	"github.com/speakeasy-api/gram/server/internal/xmcp"
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
		&cli.StringFlag{
			Name:     "external-mcp-oauth-redirect-domains",
			Usage:    "Comma separated list of allowed redirect domains for external MCP OAuth flows. Useful when using ngrok, tailscale, or some other custom host for local development.",
			EnvVars:  []string{"GRAM_EXTERNAL_MCP_OAUTH_REDIRECT_DOMAINS"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "workos-api-key",
			Usage:    "WorkOS API key for user identity lookups",
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
	flags = append(flags, pluginsFlags...)
	flags = append(flags, assistantRuntimeFlags...)
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
				attr.SlogComponent("server"),
				attr.SlogServiceName(serviceName),
				attr.SlogServiceVersion(shortGitSHA()),
				attr.SlogServiceEnv(serviceEnv),
			)
			tracerProvider := otel.GetTracerProvider()
			meterProvider := otel.GetMeterProvider()
			slog.SetDefault(logger)

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

			guardianPolicy, err := newGuardianPolicy(c, tracerProvider)
			if err != nil {
				return err
			}

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

			chDB, shutdown, err := newClickhouseClient(ctx, logger, c)
			if err != nil {
				return fmt.Errorf("failed to connect to clickhouse database: %w", err)
			}
			shutdownFuncs = append(shutdownFuncs, shutdown)

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
				enableTracing: c.Bool("redis-enable-tracing"),
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

			workosClient, workosAvailable, err := newWorkOSClient(guardianPolicy, c)
			if err != nil {
				return fmt.Errorf("failed to create WorkOS client: %w", err)
			}

			workosEventsClient, err := newWorkOSEventsClient(c, guardianPolicy)
			if err != nil {
				return fmt.Errorf("failed to create WorkOS events client: %w", err)
			}

			billingRepo, billingTracker, err := newBillingProvider(ctx, logger, tracerProvider, guardianPolicy, redisClient, posthogClient, c)
			if err != nil {
				return fmt.Errorf("failed to create billing provider: %w", err)
			}

			speakeasyIDPClient := speakeasyclient.NewClient(
				logger,
				tracerProvider,
				guardianPolicy,
				c.String("speakeasy-server-address"),
				c.String("speakeasy-secret-key"),
				db,
				conv.Ternary(workosAvailable, workosClient, nil),
				posthogClient,
			)

			sessionManager := sessions.NewManager(
				logger,
				tracerProvider,
				guardianPolicy,
				db,
				redisClient,
				cache.SuffixNone,
				c.String("speakeasy-server-address"),
				c.String("speakeasy-secret-key"),
				pylonClient,
				posthogClient,
				billingRepo,
				conv.Ternary(workosAvailable, workosClient, nil),
				speakeasyIDPClient,
			)

			chatSessionsManager := chatsessions.NewManager(logger, redisClient, c.String("jwt-signing-key"))

			encryptionClient, err := encryption.New(c.String("encryption-key"))
			if err != nil {
				return fmt.Errorf("failed to create encryption client: %w", err)
			}

			mcpMetadataRepo := mcpmetadata_repo.New(db)
			env := environments.NewEnvironmentEntries(logger, db, encryptionClient, mcpMetadataRepo)

			k8sClient, err := k8s.InitializeK8sClient(ctx, logger, c.String("environment"))
			if err != nil {
				return fmt.Errorf("failed to create kubernetes client: %w", err)
			}

			temporalEnv, shutdown, err := newTemporalClient(logger, temporalClientOptions{
				address:      c.String("temporal-address"),
				namespace:    c.String("temporal-namespace"),
				taskQueue:    c.String("temporal-task-queue"),
				certPEMBlock: []byte(c.String("temporal-client-cert")),
				keyPEMBlock:  []byte(c.String("temporal-client-key")),
			})
			if err != nil {
				return fmt.Errorf("failed to create temporal client: %w", err)
			}

			if temporalEnv == nil {
				return errors.New("insufficient options to create temporal client")
			}
			shutdownFuncs = append(shutdownFuncs, shutdown)

			auditLogger := newAuditLogger()

			productFeatures := productfeatures.NewClient(logger, tracerProvider, db, redisClient)

			var openRouter openrouter.Provisioner
			if c.String("environment") == "local" {
				openRouter = openrouter.NewDevelopment(c.String("openrouter-dev-key"))
			} else {
				openRouter = openrouter.New(
					logger,
					tracerProvider,
					guardianPolicy,
					db,
					c.String("environment"),
					c.String("openrouter-provisioning-key"),
					&background.OpenRouterKeyRefresher{TemporalEnv: temporalEnv},
					productFeatures,
					billingTracker,
				)
			}

			serverURL, err := url.Parse(c.String("server-url"))
			if err != nil {
				return fmt.Errorf("failed to parse server url: %w", err)
			}

			externalMcpOAuthConfig := oauth.ExternalOAuthServiceConfig{
				ServerURL:            serverURL,
				AllowedRedirectHosts: []string{},
			}

			redirectDomains := c.String("external-mcp-oauth-redirect-domains")
			if redirectDomains == "" {
				// Default: allow server's own hostname
				externalMcpOAuthConfig.AllowedRedirectHosts = []string{serverURL.Hostname()}
			} else {
				for host := range strings.SplitSeq(redirectDomains, ",") {
					host = strings.TrimSpace(host)
					if host == "" {
						continue // skip empty entries from trailing commas
					}
					externalMcpOAuthConfig.AllowedRedirectHosts = append(
						externalMcpOAuthConfig.AllowedRedirectHosts,
						host,
					)
				}
				if len(externalMcpOAuthConfig.AllowedRedirectHosts) == 0 {
					return errors.New("no valid hosts in external-mcp-oauth-redirect-domains")
				}
			}

			siteURL, err := url.Parse(c.String("site-url"))
			if err != nil {
				return fmt.Errorf("failed to parse site url: %w", err)
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
			roleClient, err := newAccessRoleProvider(ctx, logger, guardianPolicy, c)
			if err != nil {
				return fmt.Errorf("failed to create access role provider: %w", err)
			}
			authzEngine := authz.NewEngine(
				logger,
				db,
				chDB,
				rbacEnabled,
				challengeLoggingEnabled,
				roleClient,
				cache.NewRedisCacheAdapter(redisClient),
				authz.EngineOpts{DevMode: c.String("environment") == "local"},
			)

			telemLogger, shutdown := newTelemetryLogger(ctx, logger, chDB, logsEnabled, toolIOLogsEnabled)
			shutdownFuncs = append(shutdownFuncs, shutdown)

			telemSvc := tm.NewService(logger, tracerProvider, db, chDB, sessionManager, chatSessionsManager, logsEnabled, sessionCaptureEnabled, posthogClient, authzEngine)

			// Wrap cache for hooks service in local development
			var hooksCache cache.Cache = cache.NewRedisCacheAdapter(redisClient)
			if c.String("environment") == "local" {
				hooksCache = hooks.NewLocalSessionCache(hooksCache, db)
			}

			chatWriter, chatWriterShutdown := chat.NewChatMessageWriter(logger, db, assetStorage)
			shutdownFuncs = append(shutdownFuncs, chatWriterShutdown)

			captureStrategy := chat.NewChatMessageCaptureStrategy(logger, db, chatWriter)

			completionsClient := openrouter.NewUnifiedClient(
				logger,
				guardianPolicy,
				openRouter,
				captureStrategy,
				chat.NewDefaultUsageTrackingStrategy(db, logger, openRouter, billingTracker, &background.FallbackModelUsageTracker{TemporalEnv: temporalEnv}),
				&background.TemporalChatTitleGenerator{TemporalEnv: temporalEnv},
				&background.TemporalDelayedChatResolutionAnalyzer{TemporalEnv: temporalEnv},
				telemLogger,
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

			authorizer := auth.New(logger, db, sessionManager, authzEngine)
			assistantTokenManager := assistanttokens.New(c.String("jwt-signing-key"), db, authzEngine)
			assistantRuntime, err := newAssistantRuntime(ctx, logger, tracerProvider, c, guardianPolicy, db, serverURL)
			if err != nil {
				return err
			}
			oauthService := oauth.NewService(logger, tracerProvider, meterProvider, db, serverURL, cache.NewRedisCacheAdapter(redisClient), encryptionClient, env, sessionManager, guardianPolicy)
			externalOAuthService := oauth.NewExternalOAuthService(logger, guardianPolicy, db, cache.NewRedisCacheAdapter(redisClient), authorizer, encryptionClient, externalMcpOAuthConfig)
			shadowMCPClient := shadowmcp.NewClient(logger, db, cache.NewRedisCacheAdapter(redisClient))
			triggerApp := newTriggersApp(logger, db, encryptionClient, temporalEnv, telemLogger, serverURL)

			platformSvc := platformtoolsruntime.NewService(
				logger,
				db,
				telemSvc,
				auditLogger,
				platformtoolsruntime.WithTriggerTools(triggerApp),
				platformtoolsruntime.WithSlackHTTPClient(guardianPolicy.PooledClient()),
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
				telemLogger,
				telemSvc,
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
			contextWindowResolver := openrouter.NewContextWindowResolver(logger, guardianPolicy, cache.NewRedisCacheAdapter(redisClient))
			chatService := chat.NewService(logger, tracerProvider, db, sessionManager, chatSessionsManager, openRouter, chatClient, contextWindowResolver, posthogClient, telemSvc, assetStorage, authzEngine, assistantTokenManager, billingRepo)
			assistantsCore := assistants.NewServiceCore(logger, tracerProvider, db, assistantRuntime, slackClient, assistantTokenManager, serverURL, telemLogger)
			assistantsSvc := assistants.NewService(logger, tracerProvider, db, sessionManager, authzEngine, assistantsCore, &background.AssistantWorkflowSignaler{TemporalEnv: temporalEnv})
			triggerApp.RegisterDispatcher(assistantsSvc)

			toolsetsSvc := toolsets.NewService(logger, tracerProvider, db, sessionManager, cache.NewRedisCacheAdapter(redisClient), authzEngine, auditLogger)
			mcpMetadataService := mcpmetadata.NewService(logger, tracerProvider, db, sessionManager, serverURL, siteURL, cache.NewRedisCacheAdapter(redisClient), authzEngine, auditLogger)

			// Construct the GitHub App client once; share with the plugin publish
			// flow and the marketplace proxy so they hit the same token cache and
			// the same App identity. nil when the App isn't configured.
			var ghClient *ghclient.Client
			if appID, key := c.Int64("plugins-github-app-id"), c.String("plugins-github-private-key"); appID != 0 && key != "" {
				ghClient, err = ghclient.NewClient(appID, []byte(key), guardianPolicy.Client())
				if err != nil {
					return fmt.Errorf("create github app client: %w", err)
				}
			}

			// Marketplace proxy routes (URL-based marketplace.json + git Smart
			// HTTP for plugin source clones). Mounted via the outermost
			// mux.Use middleware so /m/ and /p/ paths short-circuit the Goa
			// mux. Public base URL is server-url by definition — the proxy
			// lives on this server, so the plugin sources we embed in the
			// rendered manifest must point back at it. nil when no App is
			// configured.
			//
			// We wrap the proxy with the recovery middleware before mounting:
			// the dispatch happens inside the outermost mux.Use, ahead of the
			// chain-level recovery, so without this wrap a panic in any
			// marketplace handler (or the DB resolver) would crash the
			// server process.
			var (
				marketplaceServer *marketplace.Server
				marketplaceRoutes http.Handler
			)
			if ghClient != nil {
				marketplaceServer = marketplace.NewServer(
					marketplace.NewDBResolver(db, ghClient),
					guardianPolicy.Client(),
					c.String("server-url"),
					logger,
				)
				marketplaceRoutes = middleware.NewRecovery(logger)(marketplaceServer.Routes())
				logger.InfoContext(ctx, "marketplace proxy: enabled",
					attr.SlogServerAddress(c.String("address")),
				)
			} else {
				logger.InfoContext(ctx, "marketplace proxy: disabled (no github app configured)")
			}

			mux := goahttp.NewMuxer()
			mux.Use(func(h http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet && r.URL.Path == "/healthz" {
						w.WriteHeader(http.StatusOK)
						return
					}
					if marketplaceServer != nil && marketplaceServer.IsMarketplaceRoute(r) {
						marketplaceRoutes.ServeHTTP(w, r)
						return
					}

					h.ServeHTTP(w, r)
				})
			})
			mux.Use(func(h http.Handler) http.Handler {
				return otelhttp.NewHandler(h, "http", otelhttp.WithServerName("gram"))
			})
			mux.Use(middleware.RouteLabelerMiddleware)
			mux.Use(middleware.NewHTTPLoggingMiddleware(logger))
			mux.Use(middleware.NewRecovery(logger))
			mux.Use(middleware.CORSMiddleware(c.String("environment"), c.String("server-url"), chatSessionsManager))
			mux.Use(customdomains.Middleware(logger, db, c.String("environment"), serverURL))
			mux.Use(middleware.SessionMiddleware)
			mux.Use(middleware.AdminOverrideMiddleware)
			mux.Use(middleware.RBACOverrideMiddleware())

			// Reuse the same Presidio client the worker uses for offline analysis
			// so the runtime hook scanner can flag/redact PII inputs too.
			var hookPIIScanner risk_analysis.PIIScanner
			if presidioURL := c.String("presidio-analyzer-url"); presidioURL != "" {
				hookPIIScanner = risk_analysis.NewPresidioClient(presidioURL, tracerProvider, meterProvider, logger)
			}

			riskScanner, err := risk.NewScanner(logger, db, hookPIIScanner, meterProvider)
			if err != nil {
				return fmt.Errorf("create risk scanner: %w", err)
			}

			about.Attach(mux, about.NewService(logger, tracerProvider))
			access.Attach(mux, access.NewService(logger, tracerProvider, db, chDB, sessionManager, roleClient, authzEngine, productFeatures, auditLogger))
			assistants.Attach(mux, assistantsSvc)
			hooks.Attach(mux, hooks.NewService(logger, db, tracerProvider, telemLogger, sessionManager, hooksCache, chatClient, temporalEnv, authzEngine, productFeatures, &background.TemporalChatTitleGenerator{TemporalEnv: temporalEnv}, riskScanner, shadowMCPClient, chatWriter))
			auditapi.Attach(mux, auditapi.NewService(logger, tracerProvider, db, sessionManager, authzEngine))
			auth.Attach(mux, auth.NewService(
				logger,
				tracerProvider,
				db,
				sessionManager,
				auth.AuthConfigurations{
					SpeakeasyServerAddress: c.String("speakeasy-server-address"),
					GramServerURL:          c.String("server-url"),
					SignInRedirectURL:      auth.FormSignInRedirectURL(c.String("site-url")),
					Environment:            c.String("environment"),
				},
				authzEngine,
				billingRepo,
				&background.TemporalAssistantsSubscriptionCancelScheduler{TemporalEnv: temporalEnv},
			))
			organizations.Attach(mux, organizations.NewService(logger, tracerProvider, db, sessionManager, workosClient, productFeatures, authzEngine))
			projects.Attach(mux, projects.NewService(logger, tracerProvider, db, sessionManager, authzEngine, auditLogger))
			packages.Attach(mux, packages.NewService(logger, tracerProvider, db, sessionManager, authzEngine))

			pluginsGitHub, err := plugins.NewGitHubConfig(plugins.GitHubConfigInput{
				Client:         ghClient,
				Org:            c.String("plugins-github-org"),
				InstallationID: c.Int64("plugins-github-installation-id"),
			})
			if err != nil {
				return fmt.Errorf("plugins github config: %w", err)
			}
			if pluginsGitHub != nil {
				logger.InfoContext(ctx, "GitHub publishing for plugins: enabled")
			} else {
				logger.InfoContext(ctx, "GitHub publishing for plugins: disabled")
			}
			plugins.Attach(mux, plugins.NewService(logger, tracerProvider, db, sessionManager, authzEngine, auditLogger, pluginsGitHub, c.String("environment"), c.String("server-url")))
			productfeatures.Attach(mux, productfeatures.NewService(logger, tracerProvider, db, sessionManager, redisClient, authzEngine))
			toolsets.Attach(mux, toolsetsSvc)
			integrations.Attach(mux, integrations.NewService(logger, tracerProvider, db, sessionManager, authzEngine))
			templates.Attach(mux, templates.NewService(logger, tracerProvider, db, sessionManager, toolsetsSvc, authzEngine, auditLogger))
			assets.Attach(mux, assets.NewService(logger, tracerProvider, guardianPolicy, db, sessionManager, chatSessionsManager, assetStorage, c.String("jwt-signing-key"), authzEngine, auditLogger))
			deployments.Attach(mux, deployments.NewService(logger, tracerProvider, db, temporalEnv, sessionManager, assetStorage, posthogClient, siteURL, mcpRegistryClient, authzEngine, auditLogger))
			keys.Attach(mux, keys.NewService(logger, tracerProvider, db, sessionManager, c.String("environment"), authzEngine, auditLogger))
			chatsessionssvc.Attach(mux, chatsessionssvc.NewService(logger, tracerProvider, db, sessionManager, chatSessionsManager, authzEngine))
			environments.Attach(mux, environments.NewService(logger, tracerProvider, db, sessionManager, encryptionClient, authzEngine, auditLogger))
			mcpservers.Attach(mux, mcpservers.NewService(logger, tracerProvider, db, sessionManager, authzEngine, auditLogger))
			mcpendpoints.Attach(mux, mcpendpoints.NewService(logger, tracerProvider, db, sessionManager, authzEngine, auditLogger))
			usersessions.Attach(mux, usersessions.NewService(logger, tracerProvider, db, sessionManager, chatSessionsManager, authzEngine))
			remotemcp.Attach(mux, remotemcp.NewService(logger, tracerProvider, db, sessionManager, encryptionClient, authzEngine, guardianPolicy, auditLogger))
			xmcp.Attach(mux, xmcp.NewService(logger, tracerProvider, meterProvider, db, encryptionClient, authzEngine, guardianPolicy, billingRepo, billingTracker, mcpService, serverURL), mcpMetadataService)
			triggers.Attach(mux, triggers.NewService(logger, tracerProvider, db, sessionManager, authzEngine, triggerApp, auditLogger))
			tools.Attach(mux, tools.NewService(logger, tracerProvider, db, sessionManager, authzEngine))
			resources.Attach(mux, resources.NewService(logger, tracerProvider, db, sessionManager, authzEngine))
			oauth.AttachExternalOAuth(mux, externalOAuthService)
			oauth.Attach(mux, oauthService)
			instances.Attach(mux, instances.NewService(logger, tracerProvider, meterProvider, db, sessionManager, chatSessionsManager, env, encryptionClient, cache.NewRedisCacheAdapter(redisClient), guardianPolicy, functionsOrchestrator, platformSvc, billingTracker, telemLogger, productFeatures, serverURL, authzEngine))
			mcpmetadata.Attach(mux, mcpMetadataService)
			externalmcp.Attach(mux, externalmcp.NewService(logger, tracerProvider, db, sessionManager, mcpRegistryClient, authzEngine))
			collections.Attach(mux, collections.NewService(logger, tracerProvider, db, sessionManager, authzEngine, serverURL))
			mcp.Attach(mux, mcpService, mcpMetadataService)
			chat.Attach(mux, chatService)
			variations.Attach(mux, variations.NewService(logger, tracerProvider, db, sessionManager, authzEngine, auditLogger))
			customdomains.Attach(mux, customdomains.NewService(logger, tracerProvider, db, sessionManager, &background.CustomDomainRegistrationClient{TemporalEnv: temporalEnv}, authzEngine, auditLogger))
			usage.Attach(mux, usage.NewService(logger, tracerProvider, db, sessionManager, billingRepo, serverURL, posthogClient, openRouter, authzEngine))
			tm.Attach(mux, telemSvc)
			functions.Attach(mux, functions.NewService(logger, tracerProvider, db, encryptionClient, tigrisStore))

			riskSignaler := background.NewThrottledSignaler(
				&background.TemporalRiskAnalysisSignaler{TemporalEnv: temporalEnv, Logger: logger},
				30*time.Second,
				logger,
			)
			shutdownFuncs = append(shutdownFuncs, riskSignaler.Shutdown)
			riskService := risk.NewService(logger, tracerProvider, db, sessionManager, authzEngine, riskSignaler, completionsClient, shadowMCPClient, auditLogger)
			chatWriter.AddObserver(riskService)
			risk.Attach(mux, riskService)

			slack.Attach(mux, slack.NewService(logger, tracerProvider, db, sessionManager, encryptionClient, redisClient, slackClient, temporalEnv, slack.Configurations{
				GramServerURL:     c.String("server-url"),
				GramSiteURL:       c.String("site-url"),
				SignInRedirectURL: auth.FormSignInRedirectURL(c.String("site-url")),
			}, authzEngine))

			srv := &http.Server{
				Addr:              c.String("address"),
				Handler:           mux,
				ReadHeaderTimeout: 10 * time.Second,
				BaseContext: func(net.Listener) context.Context {
					return ctx
				},
			}

			sigctx, sigcancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
			defer sigcancel()

			group := pool.New()

			if c.Bool("dev-single-process") {
				workerInterruptCh := make(chan any)
				group.Go(func() {
					<-sigctx.Done()
					close(workerInterruptCh)
				})
				group.Go(func() {
					var piiScanner risk_analysis.PIIScanner = &risk_analysis.StubPIIScanner{}
					if presidioURL := c.String("presidio-analyzer-url"); presidioURL != "" {
						piiScanner = risk_analysis.NewPresidioClient(presidioURL, tracerProvider, meterProvider, logger)
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
						TelemetryLogger:     telemLogger,
						TriggersApp:         triggerApp,
						CacheAdapter:        cache.NewRedisCacheAdapter(redisClient),
						AssistantsCore:      assistantsCore,
						TemporalEnv:         temporalEnv,
						PIIScanner:          piiScanner,
						ShadowMCPClient:     shadowMCPClient,
						AuditLogger:         auditLogger,
						WorkOSEventsClient:  workosEventsClient,
					})
					if err := temporalWorker.Run(workerInterruptCh); err != nil {
						logger.ErrorContext(ctx, "temporal worker failed", attr.SlogError(err))
					}
				})
			}

			group.Go(func() {
				<-sigctx.Done()

				logger.InfoContext(ctx, "shutting down server")

				graceCtx, graceCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
				defer graceCancel()

				if err := srv.Shutdown(graceCtx); err != nil {
					logger.ErrorContext(ctx, "failed to shutdown development server", attr.SlogError(err))
				}
			})

			tlsEnabled := c.String("ssl-key-file") != "" && c.String("ssl-cert-file") != ""

			{
				controlServer := control.Server{
					Address:          c.String("control-address"),
					Logger:           logger.With(attr.SlogComponent("control")),
					DisableProfiling: false,
				}

				temporals := []*o11y.NamedResource[client.Client]{
					{Name: "default", Resource: temporalEnv.Client()},
				}

				listenAddr := srv.Addr
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
						return fmt.Errorf("failed to read TLS certificate for health check: %w", err)
					}
					healthzEndpoint.TLSCertificate = cert
				}
				shutdown, err := controlServer.Start(c.Context, o11y.NewHealthCheckHandler(
					[]*o11y.NamedResource[*o11y.HTTPEndpoint]{{Name: "api", Resource: healthzEndpoint}},
					[]*o11y.NamedResource[*pgxpool.Pool]{{Name: "default", Resource: db}},
					[]*o11y.NamedResource[*redis.Client]{{Name: "default", Resource: redisClient}},
					temporals,
				))
				if err != nil {
					return fmt.Errorf("failed to start control server: %w", err)
				}

				shutdownFuncs = append(shutdownFuncs, shutdown)
			}

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
