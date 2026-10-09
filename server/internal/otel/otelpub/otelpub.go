// Package otelpub is how the server's own code writes log records into the OTel pipeline.
package otelpub

import (
	"log/slog"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// NewLoggerProvider returns a logger provider whose Emit publishes each record to the inbound log topic before returning.
func NewLoggerProvider(logger *slog.Logger, publisher gcp.Publisher[*otelv1.InboundLogRecord], res *resource.Resource) *sdklog.LoggerProvider {
	// Synchronous on purpose: a batch processor would drop queued records on shutdown.
	return sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(newProcessor(&logExporter{
			logger:    logger.With(attr.SlogComponent("otelpub")),
			publisher: publisher,
		})),
	)
}
