// Package otelsink publishes exported MCP connection logs to the otel pipeline.
package otelsink

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/dataexports"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	otelsvc "github.com/speakeasy-api/gram/server/internal/otel"
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
	return &Sink{logs: otelpub.NewLogger(publisher, connectionLogResource(serviceName), dialect.ConnectionLogScope, otelpub.WithProvenanceSource(dialect.ConnectionLogSource))}
}

func connectionLogResource(serviceName string) *resource.Resource {
	return resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceNameKey.String(serviceName))
}

func (s *Sink) Publish(ctx context.Context, organizationID, projectID, recordID string, record log.Record) gcp.PublishResult {
	return s.logs.Publish(otelpub.WithRecordID(otelpub.WithTenant(ctx, organizationID, projectID), recordID), record)
}

func (s *Sink) Close() { s.logs.Close() }

// TestSender sends a test connection event, built exactly as the sink builds
// real ones, straight to one destination. It never publishes.
type TestSender struct {
	records *otelpub.RecordBuilder
	probe   *otelsvc.ConnectionLogProbe
}

var _ dataexports.TestEventSender = (*TestSender)(nil)

func NewTestSender(probe *otelsvc.ConnectionLogProbe, serviceName string) *TestSender {
	return &TestSender{records: otelpub.NewRecordBuilder(connectionLogResource(serviceName), dialect.ConnectionLogScope, otelpub.WithProvenanceSource(dialect.ConnectionLogSource)), probe: probe}
}

func (s *TestSender) SendConnectionLogTest(ctx context.Context, destination dataexports.TestDestination) (int, error) {
	record := mcpconnectionattempts.TestExportRecord(time.Now())
	inbound, err := s.records.Prepare(otelpub.WithTenant(ctx, destination.OrganizationID, destination.ProjectID.String()), &record)
	if err != nil {
		return 0, fmt.Errorf("prepare connection log test record: %w", err)
	}
	status, err := s.probe.Send(ctx, destination, inbound)
	if err != nil {
		return 0, fmt.Errorf("send connection log test record: %w", err)
	}
	return status, nil
}
