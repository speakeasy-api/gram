package gram

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"

	"cloud.google.com/go/compute/metadata"
	"cloud.google.com/go/pubsub/v2"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"
	"google.golang.org/api/option"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/platforminit"
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

			desiredTopics, err := platforminit.DiscoverTopicsFromBytes(descriptors)
			if err != nil {
				return fmt.Errorf("failed to discover topics: %w", err)
			}

			err = platforminit.ReconcileTopics(ctx, logger, projectID, client, desiredTopics)
			if err != nil {
				return fmt.Errorf("failed to reconcile topics: %w", err)
			}

			return nil
		},
		After: func(c *cli.Context) error {
			return runShutdown(PullLogger(c.Context), c.Context, shutdownFuncs)
		},
	}
}
