package infra

import (
	"context"
	"fmt"
	"log/slog"

	"cloud.google.com/go/compute/metadata"
	"cloud.google.com/go/pubsub/v2"
	"github.com/speakeasy-api/gram/infra/internal/attr"
	"github.com/urfave/cli/v2"
	"google.golang.org/api/option"
)

func newDemoCommand() *cli.Command {
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
		Name:  "demo",
		Usage: "Run a simple pubsub demo utilizing the infra framework",
		Flags: flags,
		Action: func(c *cli.Context) error {
			ctx, cancel := context.WithCancel(c.Context)
			defer cancel()

			serviceName := "gram-infra-demo"
			serviceEnv := c.String("environment")
			logger := PullLogger(c.Context).With(
				attr.SlogServiceName(serviceName),
				attr.SlogServiceVersion(shortGitSHA()),
				attr.SlogServiceEnv(serviceEnv),
			)

			slog.SetDefault(logger)

			var err error
			var projectID string
			opts := []option.ClientOption{option.WithLogger(logger)}
			switch {
			case c.String("pubsub-emulator-host") != "":
				opts = append(opts, option.WithEndpoint(c.String("pubsub-emulator-host")), option.WithoutAuthentication())
				projectID = "my-project-id"
			default:
				projectID, err = metadata.ProjectIDWithContext(ctx)
				if err != nil {
					return fmt.Errorf("failed to get google cloud project id: %w", err)
				}
			}

			client, err := pubsub.NewClient(ctx, projectID, opts...)
			if err != nil {
				return fmt.Errorf("failed to create pubsub client: %w", err)
			}

			_ = client

			// --- DEMO BELOW - INTENDED FOR APPLICATIONS ---

			// group := pool.New()

			// broker := gcppub.NewEmulatedPubSub(logger, projectID, client, descriptors)

			// // Get a publisher handle so we can publish messages
			// pub, err := gcppub.PubSubPublisherForMessage(ctx, broker, &pingv1.Message{})
			// if err != nil {
			// 	return fmt.Errorf("failed to get publisher: %w", err)
			// }
			// // Get a subscriber handle to receive messages
			// // Read this as: "Get a handler for the outbox processor subscription to receive pingv1.Message messages"
			// sub1, err := gcppub.PubSubSubscriberForMessage(ctx, broker, &pingv1.Message{}, &pingv1.Processor{})
			// if err != nil {
			// 	return fmt.Errorf("failed to get subscriber 1: %w", err)
			// }
			// // Get another subscriber handle to receive messages
			// sub2, err := gcppub.PubSubSubscriberForMessage(ctx, broker, &pingv1.Message{}, &pingv1.Processor{})
			// if err != nil {
			// 	return fmt.Errorf("failed to get subscriber 2: %w", err)
			// }

			// group.Go(func() {
			// 	// Cancel the shared context on exit so the subscriber
			// 	// goroutines blocked in Receive unblock and group.Wait returns
			// 	// instead of hanging.
			// 	defer cancel()
			// 	for {
			// 		msg := pingv1.Message_builder{
			// 			Id:        new(uuid.Must(uuid.NewV7()).String()),
			// 			Type:      new("simulated"),
			// 			CreatedAt: timestamppb.Now(),
			// 			Payload:   []byte(`{"msg":"Hello, World!"}`),
			// 		}.Build()

			// 		_, err := pub.Publish(ctx, msg).Get(ctx)
			// 		switch {
			// 		case errors.Is(err, context.Canceled):
			// 			return
			// 		case err != nil:
			// 			logger.ErrorContext(ctx, "publish failed", attr.SlogError(err))
			// 			return
			// 		}
			// 		time.Sleep(1 * time.Second)
			// 	}
			// })

			// group.Go(func() {
			// 	defer cancel()
			// 	err := sub1.Receive(ctx, func(ctx context.Context, m *pingv1.Message, _ gcppub.MessageMetadata) error {
			// 		logger.InfoContext(ctx, "sub1: message", attr.SlogValueAny(map[string]any{
			// 			"id":      m.GetId(),
			// 			"type":    m.GetType(),
			// 			"payload": string(m.GetPayload()),
			// 		}))

			// 		return nil
			// 	})
			// 	if err != nil {
			// 		logger.ErrorContext(ctx, "sub1: receive failed", attr.SlogError(err))
			// 		return
			// 	}
			// })

			// group.Go(func() {
			// 	defer cancel()
			// 	err := sub2.Receive(ctx, func(ctx context.Context, m *pingv1.Message, _ gcppub.MessageMetadata) error {
			// 		logger.InfoContext(ctx, "sub2: message", attr.SlogValueAny(map[string]any{
			// 			"id":      m.GetId(),
			// 			"type":    m.GetType(),
			// 			"payload": string(m.GetPayload()),
			// 		}))

			// 		return nil
			// 	})
			// 	if err != nil {
			// 		logger.ErrorContext(ctx, "sub2: receive failed", attr.SlogError(err))
			// 		return
			// 	}
			// })

			// group.Wait()

			return nil
		},
		After: func(c *cli.Context) error {
			return runShutdown(PullLogger(c.Context), c.Context, shutdownFuncs)
		},
	}
}
