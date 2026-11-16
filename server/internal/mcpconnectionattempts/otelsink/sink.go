// Package otelsink publishes exported MCP connection logs to the otel pipeline.
package otelsink

import (
	"context"

	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/otelpub"
)

// Sink publishes each row under its own organization and project, never the caller's.
type Sink struct {
	logs *otelpub.Logger
}

var _ mcpconnectionattempts.ExportSink = (*Sink)(nil)

// New is the sink every recorder that exports uses.
func New(publisher gcp.Publisher[*otelv1.InboundLogRecord], serviceName string) *Sink {
	return &Sink{logs: otelpub.NewLogger(publisher,
		resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceNameKey.String(serviceName)),
		dialect.ConnectionLogScope, otelpub.WithProvenanceSource(dialect.ConnectionLogSource))}
}

func (s *Sink) Publish(ctx context.Context, organizationID, projectID, recordID string, record log.Record) gcp.PublishResult {
	return s.logs.Publish(otelpub.WithRecordID(otelpub.WithTenant(ctx, organizationID, projectID), recordID), record)
}

func (s *Sink) Close() { s.logs.Close() }
