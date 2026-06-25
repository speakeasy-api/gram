package gram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"

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

	"github.com/speakeasy-api/gram/infra/gen"
	pingv2 "github.com/speakeasy-api/gram/infra/gen/gram/ping/v2"
	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/control"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/gitleaks"
	"github.com/speakeasy-api/gram/server/internal/must"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/ping"
	"github.com/speakeasy-api/gram/server/internal/streams"
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

			guardianPolicy, err := newGuardianPolicy(c, tracerProvider)
			if err != nil {
				return err
			}
			_ = guardianPolicy

			db, err := newDBClient(ctx, logger, meterProvider, c.String("database-url"), dbClientOptions{
				enableUnsafeLogging: c.Bool("unsafe-db-log"),
				readOnly:            false,
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

			redisClient, err := newRedisClient(ctx, redisClientOptions{
				redisAddr:     c.String("redis-cache-addr"),
				redisPassword: c.String("redis-cache-password"),
				enableTracing: false,
			})
			if err != nil {
				return fmt.Errorf("failed to connect to redis: %w", err)
			}
			_ = redisClient

			_, psbroker, shutdown, err := newPubSubClient(ctx, c, logger)
			shutdownFuncs = append(shutdownFuncs, shutdown)
			if err != nil {
				return fmt.Errorf("failed to create pubsub client: %w", err)
			}

			// Gitleaks shadow-mode subscriber: re-runs the in-process gitleaks
			// scan over GitleaksAnalysis requests and publishes any matches into
			// the shared Finding topic (nothing consumes them yet).
			findingsPub, err := gcp.PubSubPublisherForMessage(ctx, psbroker, &riskv1.Finding{})
			if err != nil {
				return fmt.Errorf("failed to create pubsub publisher for risk findings: %w", err)
			}
			shutdownFuncs = append(shutdownFuncs, findingsPub.Stop)

			gitleaksHandler, err := gitleaks.NewHandler(logger, findingsPub)
			if err != nil {
				return fmt.Errorf("failed to create gitleaks handler: %w", err)
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

func receive[M proto.Message](
	g receiverGroup,
	msg M,
	subscription proto.Message,
	handler streams.Handler[M],
	options ...gcp.SubscriberOption,
) error {
	ctx := g.getContext()
	// Prepend so callers can still override the logger via options if needed.
	options = append([]gcp.SubscriberOption{gcp.WithSubscriberLogger(g.logger)}, options...)
	sub, err := gcp.PubSubSubscriberForMessage(ctx, g.broker, msg, subscription, options...)
	if err != nil {
		return fmt.Errorf("get subscriber for message %T: %T: %w", subscription, msg, err)
	}

	msgName := proto.MessageName(msg)
	if !msgName.IsValid() {
		return fmt.Errorf("invalid proto message name: %T: %s", msg, msgName)
	}

	subName := proto.MessageName(subscription)
	if !subName.IsValid() {
		return fmt.Errorf("invalid proto message name: %T: %s", subscription, subName)
	}

	ctx = contextvalues.SetPubSubSubscriberContext(ctx, contextvalues.PubSubSubscriberContext{
		TopicProtoName:        string(msgName),
		SubscriptionProtoName: string(subName),
	})

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

			err = handler.Handle(ctx, m, meta)
			switch {
			case err == nil:
				return nil
			case errors.Is(err, context.Canceled):
				return nil
			default:
				return fmt.Errorf("handle message: %w", err)
			}
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
