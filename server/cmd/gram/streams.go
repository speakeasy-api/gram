package gram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/client"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/speakeasy-api/gram/infra/gen"
	authzv1 "github.com/speakeasy-api/gram/infra/gen/gram/authz/v1"
	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	pingv2 "github.com/speakeasy-api/gram/infra/gen/gram/ping/v2"
	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	telemetryv1 "github.com/speakeasy-api/gram/infra/gen/gram/telemetry/v1"
	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/billingnotifications"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/control"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/metering"
	meteringchrepo "github.com/speakeasy-api/gram/server/internal/metering/chrepo"
	"github.com/speakeasy-api/gram/server/internal/modelkeys"
	"github.com/speakeasy-api/gram/server/internal/must"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	otelsvc "github.com/speakeasy-api/gram/server/internal/otel"
	otelchrepo "github.com/speakeasy-api/gram/server/internal/otel/chrepo"
	"github.com/speakeasy-api/gram/server/internal/ping"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/customruleanalyzer"
	"github.com/speakeasy-api/gram/server/internal/scanners/gitleaks"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	piopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptinjection/openrouter"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	ppopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy/openrouter"
	"github.com/speakeasy-api/gram/server/internal/streams"
	"github.com/speakeasy-api/gram/server/internal/subscribers"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/usage"
	"github.com/speakeasy-api/gram/server/internal/webhooks/svixrelay"
)

func newStreamsCommand() *cli.Command {
	var shutdownFuncs []func(context.Context) error

	flags := []cli.Flag{
		&cli.StringFlag{
			Name:    "control-address",
			Value:   ":8087",
			Usage:   "HTTP address to listen on",
			EnvVars: []string{"GRAM_STREAMS_CONTROL_ADDRESS"},
		},
		&cli.StringFlag{
			Name:     "environment",
			Usage:    "The current server environment", // local, dev, prod
			Required: true,
			EnvVars:  []string{"GRAM_ENVIRONMENT"},
		},
		&cli.StringFlag{
			Name:     "database-read-replica-url",
			Usage:    "Database read replica URL",
			EnvVars:  []string{"GRAM_DATABASE_READ_REPLICA_URL"},
			Required: true,
		},
		&cli.StringFlag{
			Name:     "database-url",
			Usage:    "Database URL",
			EnvVars:  []string{"GRAM_DATABASE_URL"},
			Required: true,
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
		&cli.StringFlag{
			Name:    "stripe-api-key",
			Usage:   "The Stripe API key",
			EnvVars: []string{"STRIPE_API_KEY"},
		},
		&cli.StringFlag{
			Name:    "stripe-webhook-secret",
			Usage:   "The Stripe webhook signing secret",
			EnvVars: []string{"STRIPE_WEBHOOK_SECRET"},
		},
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "stripe-price-id-tum",
			Aliases: []string{"stripe.price_id_tum"},
			Usage:   "The Stripe metered TUM price ID",
			EnvVars: []string{"STRIPE_PRICE_ID_TUM"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "stripe-meter-id-tum",
			Aliases: []string{"stripe.meter_id_tum"},
			Usage:   "The Stripe TUM billing meter ID",
			EnvVars: []string{"STRIPE_METER_ID_TUM"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "stripe-meter-event-name",
			Aliases: []string{"stripe.meter_event_name"},
			Usage:   "The Stripe TUM meter event name",
			EnvVars: []string{"STRIPE_METER_EVENT_NAME"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "stripe-portal-configuration-id",
			Aliases: []string{"stripe.portal_configuration_id"},
			Usage:   "The controlled Stripe customer portal configuration ID",
			EnvVars: []string{"STRIPE_PORTAL_CONFIGURATION_ID"},
		}),
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
		&cli.StringFlag{
			Name:     "polar-product-id-free",
			Usage:    "The product ID of the free tier in Polar",
			EnvVars:  []string{"POLAR_PRODUCT_ID_FREE"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "polar-product-id-pro",
			Usage:    "The product ID of the pro tier in Polar",
			EnvVars:  []string{"POLAR_PRODUCT_ID_PRO"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "polar-meter-id-tool-calls",
			Usage:    "The ID of the tool calls meter in Polar",
			EnvVars:  []string{"POLAR_METER_ID_TOOL_CALLS"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "polar-meter-id-servers",
			Usage:    "The ID of the servers meter in Polar",
			EnvVars:  []string{"POLAR_METER_ID_SERVERS"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "polar-meter-id-credits",
			Usage:    "The ID of the credits meter in Polar",
			EnvVars:  []string{"POLAR_METER_ID_CREDITS"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "polar-product-id-assistants",
			Usage:    "The product ID granting the assistants benefit in Polar (auto-attached on assistants-disposition signup)",
			EnvVars:  []string{"POLAR_PRODUCT_ID_ASSISTANTS"},
			Required: false,
		},
		&cli.StringSliceFlag{
			Name:     "polar-product-ids-topup",
			Usage:    "Product IDs of one-time credit top-up packs in Polar",
			EnvVars:  []string{"POLAR_PRODUCT_IDS_TOPUP"},
			Required: false,
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
			Name:    "redis-cache-addr",
			Usage:   "Address of the redis cache server",
			EnvVars: []string{"GRAM_REDIS_CACHE_ADDR"},
		},
		&cli.StringFlag{
			Name:    "redis-cache-password",
			Usage:   "Password for the redis cache server",
			EnvVars: []string{"GRAM_REDIS_CACHE_PASSWORD"},
		},
		&cli.StringSliceFlag{
			Name:     "disallowed-cidr-blocks",
			Usage:    "List of CIDR blocks to block for SSRF protection",
			EnvVars:  []string{"GRAM_DISALLOWED_CIDR_BLOCKS"},
			Required: false,
		},
		&cli.PathFlag{
			Name:     "config-file",
			Usage:    "Path to a config file to load. Supported formats are JSON, TOML and YAML.",
			EnvVars:  []string{"GRAM_CONFIG_FILE"},
			Required: false,
		},
	}

	flags = append(flags, gcpFlags()...)
	flags = append(flags, svixFlags()...)
	flags = append(flags, posthogFlags()...)
	flags = append(flags, riskIngestFlags()...)
	flags = append(flags, clickHouseFlags()...)

	return &cli.Command{
		Name:  "streams",
		Usage: "Starts topic subscribers",
		Flags: flags,
		Action: func(c *cli.Context) error {
			serviceName := "gram-streams"
			serviceEnv := c.String("environment")
			appinfo := o11y.PullAppInfo(c.Context)
			appinfo.Command = "streams"
			logger := PullLogger(c.Context).With(
				attr.SlogComponent("streams"),
				attr.SlogServiceName(serviceName),
				attr.SlogServiceVersion(shortGitSHA()),
				attr.SlogServiceEnv(serviceEnv),
			)

			// Without a signal handler the runtime kills the process on SIGTERM,
			// so the Action never returns and the After hook never runs the
			// shutdownFuncs registered below.
			ctx, stop := signal.NotifyContext(c.Context, os.Interrupt, syscall.SIGTERM)
			defer stop()

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

			tracerProvider := otel.GetTracerProvider()
			meterProvider := otel.GetMeterProvider()
			slog.SetDefault(logger)

			if len(gen.Descriptors) == 0 {
				return fmt.Errorf("embedded descriptor set is empty: cannot generate pubsub topology")
			}

			temporalEnv, shutdown, err := newTemporalClient(logger, meterProvider, temporalClientOptions{
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
			openRouterKeyRefresher := &background.OpenRouterKeyRefresher{TemporalEnv: temporalEnv}

			db, err := newDBClient(ctx, logger, meterProvider, c.String("database-url"), dbClientOptions{
				enableUnsafeLogging: c.Bool("unsafe-db-log"),
			})
			if err != nil {
				return fmt.Errorf("failed to connect to database: %w", err)
			}
			defer db.Close()

			encryptionClient, err := encryption.New(c.String("encryption-key"))
			if err != nil {
				return fmt.Errorf("failed to create encryption client: %w", err)
			}

			replicaDB, err := newDBClient(ctx, logger, meterProvider, c.String("database-read-replica-url"), dbClientOptions{
				enableUnsafeLogging: c.Bool("unsafe-db-log"),
			})
			if err != nil {
				return fmt.Errorf("failed to connect to database: %w", err)
			}
			defer replicaDB.Close()

			redisClient, err := newRedisClient(ctx, redisClientOptions{
				redisAddr:     c.String("redis-cache-addr"),
				redisPassword: c.String("redis-cache-password"),
				enableTracing: false,
			})
			if err != nil {
				return fmt.Errorf("failed to connect to redis: %w", err)
			}

			guardianPolicy, err := newGuardianPolicy(c, logger, tracerProvider, meterProvider, redisClient)
			if err != nil {
				return err
			}

			posthogClient := posthog.New(ctx, logger, c.String("posthog-api-key"), c.String("posthog-endpoint"), c.String("posthog-personal-api-key"))
			var featureFlags feature.Provider = posthogClient
			if c.String("environment") == "local" {
				featureFlags = newLocalFeatureFlags(ctx, logger, c.String("local-feature-flags-csv"))
			}

			productFeatures := productfeatures.NewClient(logger, tracerProvider, db, redisClient)
			stripeClient, err := newStripeClient(ctx, logger, guardianPolicy, c)
			if err != nil {
				return fmt.Errorf("failed to create Stripe client: %w", err)
			}

			_, billingTracker, err := newBillingProvider(ctx, logger, tracerProvider, guardianPolicy, redisClient, posthogClient, stripeClient, c)
			if err != nil {
				return fmt.Errorf("failed to create billing provider: %w", err)
			}

			var openRouter openrouter.Provisioner
			if c.String("environment") == "local" {
				openRouter = openrouter.NewDevelopment(c.String("openrouter-dev-key"))
			} else {
				openRouter = openrouter.New(logger, tracerProvider, guardianPolicy, db, c.String("environment"), c.String("openrouter-provisioning-key"), nil, productFeatures, billingTracker, encryptionClient)
			}

			completionsClient := openrouter.NewUnifiedClient(
				logger,
				guardianPolicy,
				openRouter,
				modelkeys.NewResolver(db, encryptionClient, openRouter),
				nil,
				chat.NewDefaultUsageTrackingStrategy(db, logger, billingTracker),
				nil,
				nil,
			)
			judgeRateLimiter := openrouter.NewJudgeRateLimiter(ratelimit.NewRedisStore(redisClient))

			_, psbroker, pubsubShutdown, err := newPubSubClient(ctx, c, logger)
			if err != nil {
				return fmt.Errorf("failed to create pubsub client: %w", err)
			}
			var (
				findingsPub gcp.Publisher[*riskv1.Finding]
				logPub      gcp.Publisher[*otelv1.LogRecord]
				metricPub   gcp.Publisher[*otelv1.Metric]
				spanPub     gcp.Publisher[*otelv1.Span]
			)
			shutdownFuncs = append(shutdownFuncs, func(ctx context.Context) error {
				return shutdownPubSubPublishers(ctx, pubsubShutdown, findingsPub, logPub, metricPub, spanPub)
			})

			riskFingerprinter, err := risk.ParsePepperKeyRing([]byte(c.String("risk-fingerprint-pepper-keyring")))
			if err != nil {
				return fmt.Errorf("failed to parse risk fingerprint pepper keyring: %w", err)
			}

			enableCHRiskWrites := !c.Bool("disable-clickhouse-risk-writes")
			chConn, shutdown, err := newClickhouseClient(ctx, logger, c)
			if err != nil {
				return fmt.Errorf("failed to create clickhouse client: %w", err)
			}
			shutdownFuncs = append(shutdownFuncs, shutdown)

			// Gitleaks shadow-mode subscriber: re-runs the in-process gitleaks
			// scan over GitleaksAnalysis requests and publishes any matches into
			// the shared Finding topic (nothing consumes them yet).
			findingsPub, err = gcp.PubSubPublisherForMessage(ctx, psbroker, &riskv1.Finding{})
			if err != nil {
				return fmt.Errorf("failed to create pubsub publisher for risk findings: %w", err)
			}

			gitleaksHandler := gitleaks.NewHandler(logger, findingsPub)
			promptInjectionScanner := promptinjection.NewScanner(logger, piopenrouter.New(logger, tracerProvider, meterProvider, completionsClient, judgeRateLimiter).Classify)
			promptInjectionStubScanner := promptinjection.NewScanner(logger, promptinjection.NoopClassifier)
			promptInjectionHandler := promptinjection.NewHandler(logger, meterProvider, promptInjectionScanner, promptInjectionStubScanner, findingsPub, scanners.NewAsyncShadowGate(logger, featureFlags, replicaDB))
			promptPolicyScanner := promptpolicy.NewScanner(logger, ppopenrouter.New(logger, tracerProvider, meterProvider, completionsClient, judgeRateLimiter).Evaluate)
			promptPolicyStubScanner := promptpolicy.NewScanner(logger, promptpolicy.NoopEvaluator)
			promptPolicyHandler := promptpolicy.NewHandler(logger, meterProvider, promptPolicyScanner, promptPolicyStubScanner, findingsPub, scanners.NewAsyncShadowGate(logger, featureFlags, replicaDB))

			// Custom-rules shadow-mode subscriber: loads a project's selected CEL
			// detection rules from the read replica (caching their compilation) and
			// publishes any matches into the shared Finding topic.
			scanner, err := customruleanalyzer.NewScanner(replicaDB)
			if err != nil {
				return fmt.Errorf("failed to create custom rules scanner: %w", err)
			}
			customRulesHandler := customruleanalyzer.NewHandler(logger, scanner, findingsPub)

			{
				controlServer := control.Server{
					Address:          c.String("control-address"),
					Logger:           logger.With(attr.SlogComponent("control")),
					DisableProfiling: false,
				}

				shutdown, err := controlServer.Start(c.Context, o11y.NewHealthCheckHandler(
					[]*o11y.NamedResource[*o11y.HTTPEndpoint]{},
					[]*o11y.NamedResource[*pgxpool.Pool]{{Name: "read-replica", Resource: replicaDB}},
					[]*o11y.NamedResource[*redis.Client]{{Name: "default", Resource: redisClient}},
					[]*o11y.NamedResource[client.Client]{{Name: "default", Resource: temporalEnv.Client()}},
				))
				if err != nil {
					return fmt.Errorf("failed to start control server: %w", err)
				}

				shutdownFuncs = append(shutdownFuncs, shutdown)
			}

			// Use errgroup.WithContext (not a bare errgroup.Group) so the first
			// receiver or publisher to return a non-nil error cancels gctx and unwinds the rest.
			// A plain group's Wait blocks until *every* goroutine returns, and
			// the heartbeat publisher loops until its context is cancelled — so
			// a subscriber whose Receive returns (e.g. its subscription vanished
			// after the emulator restarted) would be recorded as failed but the
			// process would keep running on the eternal publisher, leaving the
			// dead subscriber silently un-restarted. Cancelling on first exit
			// lets Wait return, the process exit, and the supervisor restart us
			// so subscriptions get reconciled afresh.
			group, gctx := errgroup.WithContext(ctx)
			rg := receiverGroup{
				group:      group,
				getContext: func() context.Context { return gctx },
				tracer:     tracerProvider.Tracer("github.com/speakeasy-api/gram/server/cmd/gram/streams"),
				logger:     logger,
				broker:     psbroker,
			}

			svixClient, svixShutdown, err := newSvixClient(c, logger, guardianPolicy)
			if err != nil {
				return fmt.Errorf("failed to create svix client: %w", err)
			}
			shutdownFuncs = append(shutdownFuncs, svixShutdown)

			svixRelayHandler := svixrelay.NewHandler(logger, meterProvider, db, svixClient)
			paygKeyRefreshHandler := usage.NewPaygKeyRefreshHandler(logger, openRouterKeyRefresher)
			billingNotificationHandler := billingnotifications.NewEventHandler(logger, &background.TemporalBillingEmailScheduler{TemporalEnv: temporalEnv})
			webhookEventHandler := streams.HandlerFunc[*webhooksv1.Event](func(ctx context.Context, event *webhooksv1.Event, metadata gcp.MessageMetadata) error {
				var handlerErrors []error
				if err := svixRelayHandler.Handle(ctx, event, metadata); err != nil {
					handlerErrors = append(handlerErrors, fmt.Errorf("relay webhook event to Svix: %w", err))
				}
				if err := paygKeyRefreshHandler.Handle(ctx, event, metadata); err != nil {
					handlerErrors = append(handlerErrors, fmt.Errorf("schedule PAYG key refresh: %w", err))
				}
				if err := billingNotificationHandler.Handle(ctx, event, metadata); err != nil {
					handlerErrors = append(handlerErrors, fmt.Errorf("schedule billing notification: %w", err))
				}
				return errors.Join(handlerErrors...)
			})

			logPub, err = gcp.PubSubPublisherForMessage(ctx, psbroker, &otelv1.LogRecord{})
			if err != nil {
				return fmt.Errorf("failed to create pubsub publisher for otel logs: %w", err)
			}

			metricPub, err = gcp.PubSubPublisherForMessage(ctx, psbroker, &otelv1.Metric{})
			if err != nil {
				return fmt.Errorf("failed to create pubsub publisher for otel metrics: %w", err)
			}

			spanPub, err = gcp.PubSubPublisherForMessage(ctx, psbroker, &otelv1.Span{})
			if err != nil {
				return fmt.Errorf("failed to create pubsub publisher for otel spans: %w", err)
			}

			logRelayHandler := otelsvc.NewLogRelayHandler(
				logger,
				meterProvider,
				replicaDB,
				encryptionClient,
				guardianPolicy,
				featureFlags,
			)

			metricRelayHandler := otelsvc.NewMetricRelayHandler(
				logger,
				meterProvider,
				replicaDB,
				encryptionClient,
				guardianPolicy,
			)

			spanRelayHandler := otelsvc.NewSpanRelayHandler(
				logger,
				meterProvider,
				replicaDB,
				encryptionClient,
				guardianPolicy,
			)

			// Start subscription receivers in this block
			{
				mustReceive(rg, &pingv2.Message{}, &pingv2.Processor{}, ping.NewHandler(logger, slog.LevelDebug))

				mustReceive(rg, &riskv1.GitleaksAnalysis{}, &riskv1.GitleaksAnalyzer{}, gitleaksHandler)
				mustReceive(rg, &riskv1.PromptInjectionAnalysis{}, &riskv1.PromptInjectionAnalyzer{}, promptInjectionHandler)
				mustReceive(rg, &riskv1.PromptPolicyAnalysis{}, &riskv1.PromptPolicyAnalyzer{}, promptPolicyHandler)
				mustReceive(rg, &riskv1.CustomRulesAnalysis{}, &riskv1.CustomRulesAnalyzer{}, customRulesHandler)

				mustReceive(rg, &telemetryv1.LogRecord{}, &telemetryv1.Noop{}, new(subscribers.NoopHandler[*telemetryv1.LogRecord]))

				mustReceive(rg, &webhooksv1.Event{}, &webhooksv1.SvixRelay{}, webhookEventHandler)

				mustReceive(rg, &authzv1.Challenge{}, &authzv1.ChallengeCHWriter{}, authz.NewChallengeCHWriter(logger, chConn))
				mustReceiveBatch(rg, &meteringv1.MeterReading{}, &meteringv1.MeterReadingCHWriter{}, metering.NewMeterReadingCHWriter(logger, db, meteringchrepo.New(chConn)), gcp.BatchReceiveSettings{MaxMessages: 1000, MaxBytes: 10 * constants.MiB, MaxLatency: time.Second})

				mustReceive(rg, &otelv1.InboundLogRecord{}, &otelv1.InboundLogRecordTransformer{}, otelsvc.NewLogTransformHandler(
					logger,
					meterProvider,
					logPub,
					replicaDB,
					cache.NewRedisCacheAdapter(redisClient),
				))
				mustReceive(rg, &otelv1.InboundMetric{}, &otelv1.InboundMetricTransformer{}, otelsvc.NewMetricTransformHandler(
					logger,
					meterProvider,
					metricPub,
				))
				mustReceive(rg, &otelv1.InboundSpan{}, &otelv1.InboundSpanTransformer{}, otelsvc.NewSpanTransformHandler(
					logger,
					meterProvider,
					spanPub,
					replicaDB,
					cache.NewRedisCacheAdapter(redisClient),
				))
				mustReceiveBatchWithResult(rg, &otelv1.LogRecord{}, &otelv1.LogRelay{}, logRelayHandler, gcp.BatchReceiveSettings{MaxMessages: 10000, MaxBytes: 10 * constants.MiB, MaxLatency: 5 * time.Second})
				mustReceiveBatchWithResult(rg, &otelv1.Metric{}, &otelv1.MetricRelay{}, metricRelayHandler, gcp.BatchReceiveSettings{MaxMessages: 10000, MaxBytes: 10 * constants.MiB, MaxLatency: 5 * time.Second})
				mustReceiveBatchWithResult(rg, &otelv1.Span{}, &otelv1.SpanRelay{}, spanRelayHandler, gcp.BatchReceiveSettings{MaxMessages: 10000, MaxBytes: 10 * constants.MiB, MaxLatency: 5 * time.Second})

				// Event feed tee: mirror the normalized OTEL topics into the
				// otel_logs / otel_traces ClickHouse tables.
				mustReceiveBatch(rg, &otelv1.LogRecord{}, &otelv1.LogEventCHWriter{}, otelsvc.NewLogEventCHWriter(logger, meterProvider, otelchrepo.New(chConn)), gcp.BatchReceiveSettings{MaxMessages: 10000, MaxBytes: 10 * constants.MiB, MaxLatency: 5 * time.Second})
				mustReceiveBatch(rg, &otelv1.Span{}, &otelv1.SpanEventCHWriter{}, otelsvc.NewSpanEventCHWriter(logger, meterProvider, otelchrepo.New(chConn)), gcp.BatchReceiveSettings{MaxMessages: 10000, MaxBytes: 10 * constants.MiB, MaxLatency: 5 * time.Second})

				if enableCHRiskWrites {
					mustReceiveBatchWithResult(rg, &riskv1.Finding{}, &riskv1.FindingCHWriter{}, risk.NewFindingCHWriter(logger, replicaDB, meterProvider, chrepo.New(chConn), riskFingerprinter), gcp.BatchReceiveSettings{MaxMessages: 1000, MaxBytes: 10 * constants.MiB, MaxLatency: 1 * time.Second})
				}
			}

			// This is just a heartbeat publisher that validates the publisher-
			// subscriber flow is working by driving a simple message through
			// the system every N seconds and logging it in the subscriber.
			group.Go(func() error {
				if err := ping.StartPublisher(gctx, logger, psbroker); err != nil {
					return fmt.Errorf("publish pings: %w", err)
				}
				return nil
			})

			if err := group.Wait(); err != nil {
				return fmt.Errorf("streaming error: %w", err)
			}

			logger.InfoContext(c.Context, "shutdown signal received, all receivers stopped")

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

type publisherStopper interface {
	Stop(context.Context) error
}

func shutdownPubSubPublishers(
	ctx context.Context,
	closeClient func(context.Context) error,
	publishers ...publisherStopper,
) error {
	stopErrors := make([]error, len(publishers))
	var stops sync.WaitGroup
	for i, publisher := range publishers {
		if publisher == nil {
			continue
		}
		stops.Go(func() {
			stopErrors[i] = publisher.Stop(ctx)
		})
	}
	stops.Wait()

	if err := closeClient(ctx); err != nil {
		stopErrors = append(stopErrors, err)
	}
	return errors.Join(stopErrors...)
}

type receiverGroup struct {
	group      *errgroup.Group
	getContext func() context.Context
	tracer     trace.Tracer
	logger     *slog.Logger
	broker     gcp.SubscriberBroker
}

// setupSubscriber resolves the subscriber for a message/subscription pair and
// stamps the shared pubsub subscriber context. It holds the prologue common to
// receive and receiveBatch so the wiring (logger option, name validation,
// context values) cannot drift between the single-message and batch paths. The
// returned msgName/subName are validated proto message names for use as
// span/log attributes.
func setupSubscriber[M proto.Message](
	g receiverGroup,
	msg M,
	subscription proto.Message,
	options ...gcp.SubscriberOption,
) (sub gcp.Subscriber[M], msgName, subName protoreflect.FullName, ctx context.Context, err error) {
	ctx = g.getContext()
	// Prepend so callers can still override the logger via options if needed.
	options = append([]gcp.SubscriberOption{gcp.WithSubscriberLogger(g.logger)}, options...)
	sub, err = gcp.PubSubSubscriberForMessage(ctx, g.broker, msg, subscription, options...)
	if err != nil {
		return nil, "", "", nil, fmt.Errorf("get subscriber for message %T: %T: %w", subscription, msg, err)
	}

	msgName = proto.MessageName(msg)
	if !msgName.IsValid() {
		return nil, "", "", nil, fmt.Errorf("invalid proto message name: %T: %s", msg, msgName)
	}

	subName = proto.MessageName(subscription)
	if !subName.IsValid() {
		return nil, "", "", nil, fmt.Errorf("invalid proto message name: %T: %s", subscription, subName)
	}

	ctx = contextvalues.SetPubSubSubscriberContext(ctx, contextvalues.PubSubSubscriberContext{
		TopicProtoName:        string(msgName),
		SubscriptionProtoName: string(subName),
	})

	return sub, msgName, subName, ctx, nil
}

func receive[M proto.Message](
	g receiverGroup,
	msg M,
	subscription proto.Message,
	handler streams.Handler[M],
	options ...gcp.SubscriberOption,
) error {
	sub, msgName, subName, ctx, err := setupSubscriber(g, msg, subscription, options...)
	if err != nil {
		return err
	}

	g.group.Go(func() error {
		if err := sub.Receive(ctx, func(ctx context.Context, m M, meta gcp.MessageMetadata) (err error) {
			// Continue the producer's trace: extract any trace context the
			// publisher propagated through the message attributes so this span
			// is a child of the publishing span instead of the root of a fresh
			// trace. Extract uses the globally configured propagator (W3C
			// tracecontext + baggage) and leaves ctx unchanged when no trace
			// headers are present, so unpropagated messages still start a new
			// trace.
			ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(meta.Attributes))

			ctx, span := g.tracer.Start(ctx, "stream.handleMessage", trace.WithAttributes(
				attr.TopicProtoName(msgName),
				attr.SubscriptionProtoName(subName),
			))

			defer func() {
				if err != nil {
					span.RecordError(err)
					span.SetStatus(codes.Error, err.Error())
				}
				span.End()
			}()

			// Recover from panics in the handler so a single bad message returns an
			// error (triggering a nack and eventual dead-lettering) instead of
			// crashing the receive goroutine. Registered after the span defer so it
			// runs first and sets err before the span records it.
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("panic recovered in message handler: %v", r)
					g.logger.ErrorContext(ctx, "panic recovered in message handler",
						attr.SlogError(err),
						attr.SlogErrorStack(string(debug.Stack())),
					)
				}
			}()

			// A context.Canceled here means the handler was interrupted (e.g. by
			// shutdown) before finishing, so the message was not processed. Return
			// the error so it is nacked and redelivered rather than acked: mapping
			// cancellation to success would silently drop the in-flight message.
			err = handler.Handle(ctx, m, meta)
			if err != nil {
				return fmt.Errorf("handle message: %w", err)
			}
			return nil
		}); err != nil {
			return fmt.Errorf("subscriber receive error: %w", err)
		}

		return nil
	})

	return nil
}

func mustReceive[M proto.Message](
	g receiverGroup,
	msg M,
	subscription proto.Message,
	handler streams.Handler[M],
	options ...gcp.SubscriberOption,
) {
	must.Nil(receive(g, msg, subscription, handler, options...))
}

// receiveBatch is the batch counterpart to receive: it registers a
// streams.BatchHandler that processes messages in groups. It is part of the
// streams runner surface so consumers can opt into batch processing; register
// one with mustReceiveBatch in the receivers block alongside the single-message
// handlers.
func receiveBatch[M proto.Message](
	g receiverGroup,
	msg M,
	subscription proto.Message,
	handler streams.BatchHandler[M],
	settings gcp.BatchReceiveSettings,
	options ...gcp.SubscriberOption,
) error {
	sub, msgName, subName, ctx, err := setupSubscriber(g, msg, subscription, options...)
	if err != nil {
		return err
	}

	g.group.Go(func() error {
		if err := sub.ReceiveBatch(ctx, settings, func(ctx context.Context, msgs []M, metas []gcp.MessageMetadata) (err error) {
			// Unlike the single-message path we do not extract per-message trace
			// context: a batch can aggregate messages from different producer
			// traces, so there is no single parent span to continue. Start a fresh
			// span for the batch instead.
			ctx, span := g.tracer.Start(ctx, "stream.handleBatch", trace.WithAttributes(
				attr.TopicProtoName(msgName),
				attr.SubscriptionProtoName(subName),
				attr.SubscriberBatchSize(len(msgs)),
			))

			defer func() {
				if err != nil {
					span.RecordError(err)
					span.SetStatus(codes.Error, err.Error())
				}
				span.End()
			}()

			// Recover from panics in the handler so a single bad batch returns an
			// error (triggering a nack and eventual dead-lettering) instead of
			// crashing the receive goroutine. Registered after the span defer so it
			// runs first and sets err before the span records it.
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("panic recovered in batch message handler: %v", r)
					g.logger.ErrorContext(ctx, "panic recovered in batch message handler",
						attr.SlogError(err),
						attr.SlogErrorStack(string(debug.Stack())),
					)
				}
			}()

			// A context.Canceled here means the handler was interrupted (e.g. by
			// shutdown) before finishing, so the batch was not fully processed.
			// Return the error so the batch is nacked and redelivered rather than
			// acked: mapping cancellation to success would silently drop every
			// un-processed message in the batch.
			err = handler.HandleBatch(ctx, msgs, metas)
			if err != nil {
				return fmt.Errorf("handle message batch: %w", err)
			}
			return nil
		}); err != nil {
			return fmt.Errorf("subscriber receive batch error: %w", err)
		}

		return nil
	})

	return nil
}

func mustReceiveBatch[M proto.Message](
	g receiverGroup,
	msg M,
	subscription proto.Message,
	handler streams.BatchHandler[M],
	settings gcp.BatchReceiveSettings,
	options ...gcp.SubscriberOption,
) {
	must.Nil(receiveBatch(g, msg, subscription, handler, settings, options...))
}

// receiveBatchWithResult registers a BatchResultHandler whose messages can
// stage individual failures without changing the all-or-nothing BatchHandler
// contract.
func receiveBatchWithResult[M proto.Message](
	g receiverGroup,
	msg M,
	subscription proto.Message,
	handler streams.BatchResultHandler[M],
	settings gcp.BatchReceiveSettings,
	options ...gcp.SubscriberOption,
) error {
	sub, msgName, subName, ctx, err := setupSubscriber(g, msg, subscription, options...)
	if err != nil {
		return err
	}

	g.group.Go(func() error {
		if err := sub.ReceiveBatchWithResult(ctx, settings, func(ctx context.Context, msgs []gcp.BatchMessage[M]) (err error) {
			ctx, span := g.tracer.Start(ctx, "stream.handleBatch", trace.WithAttributes(
				attr.TopicProtoName(msgName),
				attr.SubscriptionProtoName(subName),
				attr.SubscriberBatchSize(len(msgs)),
			))

			defer func() {
				if err != nil {
					span.RecordError(err)
					span.SetStatus(codes.Error, err.Error())
				}
				span.End()
			}()

			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("panic recovered in batch result handler: %v", r)
					g.logger.ErrorContext(ctx, "panic recovered in batch result handler",
						attr.SlogError(err),
						attr.SlogErrorStack(string(debug.Stack())),
					)
				}
			}()

			err = handler.HandleBatchWithResult(ctx, msgs)
			if err != nil {
				return fmt.Errorf("handle message batch with result: %w", err)
			}
			if err = ctx.Err(); err != nil {
				return fmt.Errorf("handle message batch with result: %w", err)
			}
			return nil
		}); err != nil {
			return fmt.Errorf("subscriber receive batch with result error: %w", err)
		}

		return nil
	})

	return nil
}

func mustReceiveBatchWithResult[M proto.Message](
	g receiverGroup,
	msg M,
	subscription proto.Message,
	handler streams.BatchResultHandler[M],
	settings gcp.BatchReceiveSettings,
	options ...gcp.SubscriberOption,
) {
	must.Nil(receiveBatchWithResult(g, msg, subscription, handler, settings, options...))
}
