package gram

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/client"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/speakeasy-api/gram/infra/gen"
	pingv2 "github.com/speakeasy-api/gram/infra/gen/gram/ping/v2"
	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/control"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/modelkeys"
	"github.com/speakeasy-api/gram/server/internal/must"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/ping"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/scanners/customruleanalyzer"
	"github.com/speakeasy-api/gram/server/internal/scanners/gitleaks"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	piopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptinjection/openrouter"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	ppopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy/openrouter"
	"github.com/speakeasy-api/gram/server/internal/streams"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
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

	flags = append(flags, gcpFlags...)
	flags = append(flags, posthogFlags...)
	flags = append(flags, riskFlags...)

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

			tracerProvider := otel.GetTracerProvider()
			meterProvider := otel.GetMeterProvider()
			slog.SetDefault(logger)

			if len(gen.Descriptors) == 0 {
				return fmt.Errorf("embedded descriptor set is empty: cannot generate pubsub topology")
			}

			guardianPolicy, err := newGuardianPolicy(c, logger, tracerProvider, meterProvider)
			if err != nil {
				return err
			}

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
			_ = redisClient

			posthogClient := posthog.New(ctx, logger, c.String("posthog-api-key"), c.String("posthog-endpoint"), c.String("posthog-personal-api-key"))
			var featureFlags feature.Provider = posthogClient
			if c.String("environment") == "local" {
				featureFlags = newLocalFeatureFlags(ctx, logger, c.String("local-feature-flags-csv"))
			}

			productFeatures := productfeatures.NewClient(logger, tracerProvider, db, redisClient)
			_, billingTracker, err := newBillingProvider(ctx, logger, tracerProvider, guardianPolicy, redisClient, posthogClient, c)
			if err != nil {
				return fmt.Errorf("failed to create billing provider: %w", err)
			}

			var openRouter openrouter.Provisioner
			if c.String("environment") == "local" {
				openRouter = openrouter.NewDevelopment(c.String("openrouter-dev-key"))
			} else {
				openRouter = openrouter.New(logger, tracerProvider, guardianPolicy, db, c.String("environment"), c.String("openrouter-provisioning-key"), nil, productFeatures, billingTracker)
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

			_, psbroker, shutdown, err := newPubSubClient(ctx, c, logger)
			shutdownFuncs = append(shutdownFuncs, shutdown)
			if err != nil {
				return fmt.Errorf("failed to create pubsub client: %w", err)
			}

			bqClient, shutdown, err := newBigQueryClient(ctx, c, logger)
			shutdownFuncs = append(shutdownFuncs, shutdown)
			if err != nil {
				return fmt.Errorf("failed to create bigquery client: %w", err)
			}

			riskFindingsTable, err := bqTableFromSpec(bqClient, c.String("bq-risk-findings"))
			if err != nil {
				return fmt.Errorf("failed to parse BigQuery table spec: %w", err)
			}

			riskFingerprinter, err := risk.ParsePepperKeyRing([]byte(c.String("risk-fingerprint-pepper-keyring")))
			if err != nil {
				return fmt.Errorf("failed to parse risk fingerprint pepper keyring: %w", err)
			}

			// Gitleaks shadow-mode subscriber: re-runs the in-process gitleaks
			// scan over GitleaksAnalysis requests and publishes any matches into
			// the shared Finding topic (nothing consumes them yet).
			findingsPub, err := gcp.PubSubPublisherForMessage(ctx, psbroker, &riskv1.Finding{})
			if err != nil {
				return fmt.Errorf("failed to create pubsub publisher for risk findings: %w", err)
			}
			shutdownFuncs = append(shutdownFuncs, findingsPub.Stop)

			gitleaksHandler := gitleaks.NewHandler(logger, findingsPub)
			promptInjectionScanner := promptinjection.NewScanner(logger, piopenrouter.New(logger, tracerProvider, meterProvider, completionsClient, judgeRateLimiter).Classify)
			promptInjectionHandler := promptinjection.NewHandler(logger, promptInjectionScanner, findingsPub)
			promptPolicyScanner := promptpolicy.NewScanner(logger, ppopenrouter.New(logger, tracerProvider, meterProvider, completionsClient, judgeRateLimiter).Evaluate)
			promptPolicyHandler := promptpolicy.NewHandler(logger, promptPolicyScanner, findingsPub)

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
					[]*o11y.NamedResource[client.Client]{},
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

			pingLogLevel := conv.Ternary(c.String("environment") == "local", slog.LevelInfo, slog.LevelDebug)

			// Start subscription receivers in this block
			{
				mustReceive(rg, &pingv2.Message{}, &pingv2.Processor{}, ping.NewHandler(logger, pingLogLevel))
				mustReceive(rg, &riskv1.GitleaksAnalysis{}, &riskv1.GitleaksAnalyzer{}, gitleaksHandler)
				mustReceive(rg, &riskv1.PromptInjectionAnalysis{}, &riskv1.PromptInjectionAnalyzer{}, promptInjectionHandler)
				mustReceive(rg, &riskv1.PromptPolicyAnalysis{}, &riskv1.PromptPolicyAnalyzer{}, promptPolicyHandler)
				mustReceive(rg, &riskv1.CustomRulesAnalysis{}, &riskv1.CustomRulesAnalyzer{}, customRulesHandler)

				mustReceiveBatch(
					rg, &riskv1.Finding{}, &riskv1.FindingBQWriter{},
					gcp.BatchReceiveSettings{MaxMessages: 1000, MaxBytes: 10 * constants.MiB, MaxLatency: 1 * time.Second},
					risk.NewFindingBQWriter(logger, meterProvider, riskFindingsTable, featureFlags, riskFingerprinter),
				)
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
	settings gcp.BatchReceiveSettings,
	handler streams.BatchHandler[M],
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
	settings gcp.BatchReceiveSettings,
	handler streams.BatchHandler[M],
	options ...gcp.SubscriberOption,
) {
	must.Nil(receiveBatch(g, msg, subscription, settings, handler, options...))
}
