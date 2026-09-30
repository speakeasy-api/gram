package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"cloud.google.com/go/pubsub/v2"

	"github.com/speakeasy-api/gram/infra/gen"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/tunnel/metrics"
	"github.com/speakeasy-api/gram/tunnel/metricspub"
)

func startMetrics(ctx context.Context, logger *slog.Logger) (*metrics.Collector, func(context.Context) error) {
	if os.Getenv("TUNNEL_METRICS_ENABLED") != "1" {
		return nil, func(context.Context) error { return nil }
	}
	project := os.Getenv("GRAM_GCP_PROJECT_ID")
	if project == "" {
		logger.WarnContext(ctx, "tunnel metrics disabled: GRAM_GCP_PROJECT_ID missing")
		return nil, func(context.Context) error { return nil }
	}
	initCtx, cancelInit := context.WithTimeout(ctx, 5*time.Second)
	defer cancelInit()
	client, err := pubsub.NewClient(initCtx, project)
	if err != nil {
		logger.WarnContext(ctx, "tunnel metrics publisher unavailable", slog.Any("error", err))
		return nil, func(context.Context) error { return nil }
	}
	var broker gcp.PublisherBroker
	if os.Getenv("PUBSUB_EMULATOR_HOST") != "" {
		broker = gcp.NewEmulatedPubSub(logger, project, client, gen.Descriptors)
	} else {
		broker = gcp.NewPubSubBroker(logger, client, gen.Descriptors)
	}
	publisher, err := metricspub.NewPublisher(initCtx, broker)
	if err != nil {
		_ = client.Close()
		logger.WarnContext(ctx, "tunnel metrics publisher unavailable", slog.Any("error", err))
		return nil, func(context.Context) error { return nil }
	}
	collector, stop := metricspub.Start(ctx, publisher)
	return collector, func(shutdown context.Context) error {
		err := stop(shutdown)
		return errors.Join(err, client.Close())
	}
}
