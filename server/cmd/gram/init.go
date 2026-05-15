package gram

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"cloud.google.com/go/compute/metadata"
	"cloud.google.com/go/pubsub/v2"
	"github.com/google/uuid"
	"github.com/sourcegraph/conc/pool"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/timestamppb"

	eventv1 "github.com/speakeasy-api/gram/infra/outbox/event/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/infra"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

//go:embed descriptors.pb
var descriptors []byte

func newInitCommand() *cli.Command {
	var shutdownFuncs []func(context.Context) error

	flags := []cli.Flag{
		&cli.StringFlag{
			Name:    "pubsub-emulator-host",
			Usage:   "Host to use for the PubSub emulator",
			EnvVars: []string{"PUBSUB_EMULATOR_HOST"},
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
	}

	return &cli.Command{
		Name:  "init",
		Usage: "Initialize infra for the Gram service",
		Flags: flags,
		Action: func(c *cli.Context) error {
			ctx, cancel := context.WithCancel(c.Context)
			defer cancel()

			serviceName := "gram-init"
			serviceEnv := c.String("environment")
			appinfo := o11y.PullAppInfo(c.Context)
			appinfo.Command = "init"
			logger := PullLogger(c.Context).With(
				attr.SlogComponent("init"),
				attr.SlogServiceName(serviceName),
				attr.SlogServiceVersion(shortGitSHA()),
				attr.SlogServiceEnv(serviceEnv),
			)

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
			_, _ = tracerProvider, meterProvider
			slog.SetDefault(logger)

			var projectID string
			switch {
			case c.String("pubsub-emulator-host") != "":
				projectID = "my-project-id"
			default:
				projectID, err = metadata.ProjectIDWithContext(ctx)
				if err != nil {
					return fmt.Errorf("failed to get google cloud project id: %w", err)
				}
			}

			client, err := pubsub.NewClient(ctx, projectID, option.WithLogger(logger))
			if err != nil {
				return fmt.Errorf("failed to create pubsub client: %w", err)
			}

			if err := infra.ProvisionPubSub(ctx, logger, projectID, client, descriptors); err != nil {
				return fmt.Errorf("provision pubsub topics and subscriptions: %w", err)
			}

			// --- DEMO BELOW - INTENDED FOR APPLICATIONS ---

			group := pool.New()

			// Get a publisher handle so we can publish messages
			pub, _ := infra.PubSubPublisherForMessage(client, &eventv1.Event{})
			// Get a subscriber handle to receive messages
			// Read this as: "Get a handler for the outbox processor subscription to receive eventv1.Event messages"
			sub1, _ := infra.PubSubSubscriberForMessage(client, &eventv1.Event{}, &eventv1.OutboxProcessor{})
			// Get another subscriber handle to receive messages
			sub2, _ := infra.PubSubSubscriberForMessage(client, &eventv1.Event{}, &eventv1.OutboxProcessor{})

			group.Go(func() {
				for {
					var msg eventv1.Event
					msg.SetId(uuid.Must(uuid.NewV7()).String())
					msg.SetType("simulated")
					msg.SetCreatedAt(timestamppb.Now())
					msg.SetPayload([]byte(`{"msg":"Hello, World!"}`))

					_, err := pub.Publish(ctx, &msg).Get(ctx)
					switch {
					case errors.Is(err, context.Canceled):
						return
					case err != nil:
						logger.ErrorContext(ctx, "publish failed", attr.SlogError(err))
						return
					}
					time.Sleep(1 * time.Second)
				}
			})

			group.Go(func() {
				err := sub1.Receive(ctx, func(ctx context.Context, m infra.Message[*eventv1.Event]) {
					defer m.Ack()
					logger.InfoContext(ctx, "sub1: message", attr.SlogValueAny(map[string]any{
						"id":      m.Data().GetId(),
						"type":    m.Data().GetType(),
						"payload": string(m.Data().GetPayload()),
					}))
				})
				if err != nil {
					logger.ErrorContext(ctx, "sub1: receive failed", attr.SlogError(err))
					return
				}
			})

			group.Go(func() {
				err := sub2.Receive(ctx, func(ctx context.Context, m infra.Message[*eventv1.Event]) {
					defer m.Ack()
					logger.InfoContext(ctx, "sub2: message", attr.SlogValueAny(map[string]any{
						"id":      m.Data().GetId(),
						"type":    m.Data().GetType(),
						"payload": string(m.Data().GetPayload()),
					}))
				})
				if err != nil {
					logger.ErrorContext(ctx, "sub1: receive failed", attr.SlogError(err))
					return
				}
			})

			group.Wait()

			return nil
		},
		After: func(c *cli.Context) error {
			return runShutdown(PullLogger(c.Context), c.Context, shutdownFuncs)
		},
	}
}
