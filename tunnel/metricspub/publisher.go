// Package metricspub adapts bounded tunnel aggregates to the existing Pub/Sub
// pipeline. This package is never imported by the customer-side agent.
package metricspub

import (
	"context"
	"time"

	"cloud.google.com/go/pubsub/v2"

	tunnelv1 "github.com/speakeasy-api/gram/infra/gen/gram/tunnel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/tunnel/metrics"
)

func Publish(pub gcp.Publisher[*tunnelv1.MetricsSnapshot]) func(context.Context, metrics.Snapshot) error {
	return func(ctx context.Context, s metrics.Snapshot) error {
		message := &tunnelv1.MetricsSnapshot{SourceId: s.SourceID, ProducerId: s.ProducerID, BucketUnix: s.Bucket, Revision: s.Revision, Kind: s.Kind, ServerId: s.ServerID, Method: s.Method, ClientFamily: s.ClientFamily, Attempts: s.Attempts, Successes: s.Successes, Errors: s.Errors, Canceled: s.Canceled, Incomplete: s.Incomplete, LatencyBins: s.LatencyBins[:], Connections: s.Connections, Consumers: s.Consumers, Substreams: s.Substreams, ConnectionsOpened: s.ConnectionsOpened}
		_, err := pub.Publish(ctx, message).Get(ctx)
		return err
	}
}

// NewPublisher bounds SDK buffering as well as the collector's aggregate map.
func NewPublisher(ctx context.Context, broker gcp.PublisherBroker) (gcp.Publisher[*tunnelv1.MetricsSnapshot], error) {
	settings := pubsub.DefaultPublishSettings
	settings.Timeout = 2 * time.Second
	settings.DelayThreshold = 100 * time.Millisecond
	settings.CountThreshold = 100
	settings.ByteThreshold = 1 << 20
	settings.FlowControlSettings = pubsub.FlowControlSettings{MaxOutstandingMessages: 1000, MaxOutstandingBytes: 16 << 20, LimitExceededBehavior: pubsub.FlowControlSignalError}
	return gcp.PubSubPublisherForMessage(ctx, broker, &tunnelv1.MetricsSnapshot{}, gcp.WithPubSubPublishSettings(&settings))
}

// Start returns an explicit drain callback. Call it after serving has stopped and
// before closing the shared Pub/Sub client. Shutdown remains bounded by ctx.
func Start(ctx context.Context, pub gcp.Publisher[*tunnelv1.MetricsSnapshot]) (*metrics.Collector, func(context.Context) error) {
	collector := metrics.New()
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); collector.Run(runCtx, Publish(pub)) }()
	return collector, func(shutdown context.Context) error {
		cancel()
		select {
		case <-done:
		case <-shutdown.Done():
			return shutdown.Err()
		}
		collector.Flush(shutdown, Publish(pub))
		return pub.Stop(shutdown)
	}
}
