package gram

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/urfave/cli/v2"
)

const (
	workflowIDKey = "workflow_id"
	runIDKey      = "run_id"
	errorKey      = "error"
)

func newTriggerPlatformUsageMetricsCommand() *cli.Command {
	return &cli.Command{
		Name:  "trigger-platform-usage-metrics",
		Usage: "Trigger the platform usage metrics collection workflow for testing",
		Flags: []cli.Flag{
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
				Name:    "temporal-client-cert",
				Usage:   "Client cert of the Temporal server",
				EnvVars: []string{"TEMPORAL_CLIENT_CERT"},
			},
			&cli.StringFlag{
				Name:    "temporal-client-key",
				Usage:   "Client key of the Temporal server",
				EnvVars: []string{"TEMPORAL_CLIENT_KEY"},
			},
		},
		Action: func(c *cli.Context) error {
			ctx := c.Context
			logger := PullLogger(ctx)

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
				return fmt.Errorf("temporal client is nil")
			}
			defer func() {
			if err := shutdown(context.Background()); err != nil {
				logger.ErrorContext(ctx, "failed to shutdown temporal client", slog.String(errorKey, err.Error()))
			}
		}()

			metricsClient := &background.PlatformUsageMetricsClient{
				Temporal: temporalClient,
			}

			logger.InfoContext(ctx, "Starting platform usage metrics workflow...")

			workflowRun, err := metricsClient.StartCollectPlatformUsageMetrics(ctx)
			if err != nil {
				return fmt.Errorf("failed to start platform usage metrics workflow: %w", err)
			}

			logger.InfoContext(ctx, "Platform usage metrics workflow started successfully",
				slog.String(workflowIDKey, workflowRun.GetID()),
				slog.String(runIDKey, workflowRun.GetRunID()))

			fmt.Printf("✅ Platform usage metrics workflow started!\n")
			fmt.Printf("   Workflow ID: %s\n", workflowRun.GetID())
			fmt.Printf("   Run ID: %s\n", workflowRun.GetRunID())
			fmt.Printf("   You can monitor it in the Temporal Web UI\n")

			return nil
		},
	}
}