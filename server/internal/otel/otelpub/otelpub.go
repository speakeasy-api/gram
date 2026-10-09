// Package otelpub is how the server's own code writes log records into the OTel pipeline.
package otelpub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/server/internal/attr"
	otelsvc "github.com/speakeasy-api/gram/server/internal/otel"
	"github.com/speakeasy-api/gram/server/internal/otel/enrich"
)

// NewLoggerProvider returns a logger provider whose Emit publishes each record to the inbound log topic before returning.
func NewLoggerProvider(logger *slog.Logger, publisher gcp.Publisher[*otelv1.InboundLogRecord], res *resource.Resource) *sdklog.LoggerProvider {
	// Synchronous on purpose: a batch processor would drop queued records on shutdown.
	return sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewSimpleProcessor(&exporter{
			logger:    logger.With(attr.SlogComponent("otelpub")),
			publisher: publisher,
		})),
	)
}

type exporter struct {
	logger    *slog.Logger
	publisher gcp.Publisher[*otelv1.InboundLogRecord]
}

var _ sdklog.Exporter = (*exporter)(nil)

func (*exporter) Shutdown(context.Context) error { return nil }

func (*exporter) ForceFlush(context.Context) error { return nil }

// Export never returns an error: the SDK would only hand it to the global
// error handler, which logs it a second time.
func (e *exporter) Export(ctx context.Context, records []sdklog.Record) error {
	for i := range records {
		record := &records[i]
		inbound, err := e.inbound(ctx, record)
		if err == nil {
			err = otelsvc.PublishLogs(ctx, e.publisher, []*otelv1.InboundLogRecord{inbound})
		}
		if err != nil {
			e.logger.WarnContext(ctx, "log record not published", attr.SlogEvent(record.EventName()), attr.SlogError(err))
		}
	}
	return nil
}

func (e *exporter) inbound(ctx context.Context, record *sdklog.Record) (*otelv1.InboundLogRecord, error) {
	t, ok := tenantFrom(ctx)
	if !ok {
		return nil, fmt.Errorf("%w: no tenancy in context", otelsvc.ErrInvalid)
	}

	inbound, err := inboundFromSDK(record)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", otelsvc.ErrInvalid, err)
	}
	for _, attrs := range [][]*otelv1.InboundLogRecord_KeyValue{
		inbound.GetAttributes(),
		inbound.GetResource().GetAttributes(),
		inbound.GetScope().GetAttributes(),
	} {
		for _, kv := range attrs {
			if enrich.IsAgentKey(kv.GetKey()) {
				return nil, fmt.Errorf("%w: attribute %q is in the reserved namespace", otelsvc.ErrInvalid, kv.GetKey())
			}
		}
	}

	inbound.SetProvenance((&otelv1.InboundLogRecord_Provenance_builder{
		Source:         new(otelsvc.ProvenanceSource),
		OrganizationId: &t.organizationID,
		ProjectId:      &t.projectID,
	}).Build())

	// Derived before observed time is set, which differs on every emit.
	id := recordIDFrom(ctx)
	if id == "" {
		id, err = contentRecordID(inbound)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", otelsvc.ErrInvalid, err)
		}
	}
	inbound.SetRecordId(id)
	inbound.SetObservedTimeUnixNano(unixNano(record.ObservedTimestamp()))
	return inbound, nil
}

func contentRecordID(record *otelv1.InboundLogRecord) (string, error) {
	var options proto.MarshalOptions
	options.Deterministic = true
	encoded, err := options.Marshal(record)
	if err != nil {
		return "", fmt.Errorf("marshal log record for record id: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
