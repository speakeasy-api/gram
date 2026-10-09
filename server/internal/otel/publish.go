package otel

import (
	"context"
	"errors"
	"fmt"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
)

// Signal names which kind of record an export or publish carries.
type Signal string

const (
	SignalLog    Signal = "log"
	SignalMetric Signal = "metric"
	SignalTrace  Signal = "trace"
)

// ErrInvalid marks a record the pipeline refuses. Retrying it cannot succeed.
var ErrInvalid = errors.New("record refused by the ingest contract")

// Publish validates every item, publishes them all, then waits for every
// result. A nil error means the whole batch is durable on the topic; a
// validation failure wraps ErrInvalid and publishes nothing.
func Publish[M any](ctx context.Context, signal Signal, publisher gcp.Publisher[M], validate func(M) error, items []M) error {
	for i, item := range items {
		if err := validate(item); err != nil {
			return fmt.Errorf("%w: item %d: %w", ErrInvalid, i, err)
		}
	}

	// Every publish is issued before any result is awaited, so the client can
	// batch them into few RPCs.
	results := make([]gcp.PublishResult, 0, len(items))
	for _, item := range items {
		results = append(results, publisher.Publish(ctx, item))
	}

	var publishErr error
	for _, result := range results {
		if _, err := result.Get(ctx); err != nil {
			publishErr = errors.Join(publishErr, err)
		}
	}
	if publishErr != nil {
		return fmt.Errorf("publish %s records: %w", signal, publishErr)
	}
	return nil
}

// PublishLogs is Publish for log records under the log ingest contract.
func PublishLogs(ctx context.Context, publisher gcp.Publisher[*otelv1.InboundLogRecord], records []*otelv1.InboundLogRecord) error {
	return Publish(ctx, SignalLog, publisher, ValidateLogRecord, records)
}
