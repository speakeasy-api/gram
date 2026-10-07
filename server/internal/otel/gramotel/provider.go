package gramotel

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
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/otel/enrich"
)

// NewLoggerProvider returns an OpenTelemetry SDK logger provider whose only
// pipeline publishes each emitted record to the inbound log topic.
//
// It must stay synchronous. Emit returns only after its record is on the
// topic or has failed, which is what lets a caller read the outcome through
// WithResult and what makes a record survive a process that exits right
// after emitting it. Never add the SDK's batch processor here: it would
// return before publishing, lose the error, and drop queued records on
// shutdown.
func NewLoggerProvider(
	logger *slog.Logger,
	metrics *Metrics,
	publisher gcp.Publisher[*otelv1.InboundLogRecord],
	res *resource.Resource,
) *sdklog.LoggerProvider {
	return sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(&processor{
			logger:    logger.With(attr.SlogComponent("gramotel")),
			metrics:   metrics,
			publisher: publisher,
		}),
	)
}

// processor applies the ingest contract to every emitted record and
// publishes it, synchronously, through the publishing core.
type processor struct {
	logger    *slog.Logger
	metrics   *Metrics
	publisher gcp.Publisher[*otelv1.InboundLogRecord]
}

var _ sdklog.Processor = (*processor)(nil)

func (*processor) Enabled(context.Context, sdklog.EnabledParameters) bool { return true }

func (*processor) Shutdown(context.Context) error { return nil }

func (*processor) ForceFlush(context.Context) error { return nil }

// OnEmit stamps tenancy, record id, observed time and scope, refuses the
// reserved namespace, and publishes the record through the core. The outcome
// goes to the caller's WithResult mailbox and to the metrics. It returns nil
// even on failure: the SDK would hand an error to the global OTel error
// handler, which only logs it again.
func (p *processor) OnEmit(ctx context.Context, record *sdklog.Record) error {
	inbound, err := p.inbound(ctx, record)
	if err != nil {
		p.metrics.record(ctx, SignalLog, o11y.OutcomeFailure, reasonInvalid, 1)
		p.logger.WarnContext(ctx, "refused a log record emitted through gramotel", attr.SlogError(err))
		resultFrom(ctx).add(err)
		return nil
	}

	if err := PublishLogs(ctx, p.metrics, p.publisher, []*otelv1.InboundLogRecord{inbound}); err != nil {
		p.logger.WarnContext(ctx, "failed to publish a log record emitted through gramotel", attr.SlogError(err))
		resultFrom(ctx).add(err)
	}
	return nil
}

// inbound builds the record the ingest edge would have published for this
// emit.
func (p *processor) inbound(ctx context.Context, record *sdklog.Record) (*otelv1.InboundLogRecord, error) {
	t, ok := tenantFrom(ctx)
	if !ok {
		return nil, fmt.Errorf("%w: no tenancy in context", ErrInvalid)
	}

	inbound, err := inboundFromSDK(record)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	// The reserved namespace is refused wherever an attribute can sit: on the
	// record, its resource and its instrumentation scope.
	for _, attrs := range [][]*otelv1.InboundLogRecord_KeyValue{
		inbound.GetAttributes(),
		inbound.GetResource().GetAttributes(),
		inbound.GetScope().GetAttributes(),
	} {
		for _, kv := range attrs {
			if enrich.IsAgentColumnKey(kv.GetKey()) {
				return nil, fmt.Errorf("%w: attribute %q is in the reserved namespace", ErrInvalid, kv.GetKey())
			}
		}
	}

	source := ProvenanceSource
	inbound.SetProvenance((&otelv1.InboundLogRecord_Provenance_builder{
		Source:         &source,
		OrganizationId: &t.organizationID,
		ProjectId:      &t.projectID,
	}).Build())

	// The content-derived id is computed before observed time is stamped,
	// since observed time differs on every emit and would make each id unique.
	id := recordIDFrom(ctx)
	if id == "" {
		id, err = contentRecordID(inbound)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
		}
	}
	inbound.SetRecordId(id)
	inbound.SetObservedTimeUnixNano(unixNano(record.ObservedTimestamp()))
	return inbound, nil
}

// contentRecordID derives a record id from the record's own bytes, so the
// same record emitted twice gets the same id and collapses downstream.
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
