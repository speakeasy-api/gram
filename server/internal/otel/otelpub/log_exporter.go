package otelpub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/server/internal/attr"
	otelsvc "github.com/speakeasy-api/gram/server/internal/otel"
	"github.com/speakeasy-api/gram/server/internal/otel/enrich"
)

type logExporter struct {
	logger    *slog.Logger
	publisher gcp.Publisher[*otelv1.InboundLogRecord]
}

var _ sdklog.Exporter = (*logExporter)(nil)

func (*logExporter) Shutdown(context.Context) error { return nil }

func (*logExporter) ForceFlush(context.Context) error { return nil }

// Export never returns an error: the SDK would only hand it to the global
// error handler, which logs it a second time.
func (e *logExporter) Export(ctx context.Context, records []sdklog.Record) error {
	for i := range records {
		if err := e.export(ctx, &records[i]); err != nil {
			e.logger.WarnContext(ctx, "log record not published", attr.SlogEvent(records[i].EventName()), attr.SlogError(err))
		}
	}
	return nil
}

func (e *logExporter) export(ctx context.Context, record *sdklog.Record) error {
	inbound, err := e.inbound(ctx, record)
	if err != nil {
		return err
	}
	if err := otelsvc.ValidateInboundLogRecord(inbound); err != nil {
		return fmt.Errorf("validate log record: %w", err)
	}
	if _, err := e.publisher.Publish(ctx, inbound).Get(ctx); err != nil {
		return fmt.Errorf("publish log record: %w", err)
	}
	return nil
}

func (e *logExporter) inbound(ctx context.Context, record *sdklog.Record) (*otelv1.InboundLogRecord, error) {
	t, ok := tenantFrom(ctx)
	if !ok {
		return nil, errors.New("no tenancy in context")
	}

	inbound, err := inboundFromSDK(record)
	if err != nil {
		return nil, err
	}
	for _, attrs := range [][]*otelv1.InboundLogRecord_KeyValue{
		inbound.GetAttributes(),
		inbound.GetResource().GetAttributes(),
		inbound.GetScope().GetAttributes(),
	} {
		for _, kv := range attrs {
			if enrich.IsPipelineKey(kv.GetKey()) {
				return nil, fmt.Errorf("attribute %q is in the reserved namespace", kv.GetKey())
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
			return nil, err
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
