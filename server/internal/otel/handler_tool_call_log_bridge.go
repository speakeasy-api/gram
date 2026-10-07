package otel

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	telemetryv1 "github.com/speakeasy-api/gram/infra/gen/gram/telemetry/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/streams"
)

const (
	meterToolCallLogBridgeBridged = "gram.otel_bridge.tool_call_logs_bridged"
	meterToolCallLogBridgeDropped = "gram.otel_bridge.tool_call_logs_dropped"
	meterToolCallLogBridgeFailed  = "gram.otel_bridge.tool_call_logs_failed"

	// bridgeReasonPublishError is why a row was nacked: the inbound topic did
	// not take it, so the subscription redelivers it.
	bridgeReasonPublishError relayReason = "publish-error"
)

// ToolCallLogBridgeHandler republishes the tool call records Gram writes
// when it runs a tool as inbound OTel log records, so they go through the
// same transform as every other producer's records and land in agent_events.
//
// Those rows never pass the OTLP ingest edge: telemetry.Logger writes them
// to ClickHouse and mirrors them onto gram.telemetry.v1.LogRecord. The
// bridge does for them what the ingest edge does for a producer's export: a
// record id, an observed time, the resource, the scope and the tenancy. The
// record id is the telemetry row id, so a redelivery collapses at read time
// the way every other record's does, and the observed time is taken from
// the row so a redelivery stamps the same value.
//
// Reliable, not best-effort: a row the inbound topic refuses is nacked and
// redelivered. Only rows that cannot be bridged at all (no tenancy, an
// unreadable payload, a record over the size budget) are dropped and
// counted, since redelivery cannot fix them. Hook rows share this topic and
// already enter the pipeline through the hooks tee, so they are left alone.
type ToolCallLogBridgeHandler struct {
	logger    *slog.Logger
	publisher gcp.Publisher[*otelv1.InboundLogRecord]
	bridged   metric.Int64Counter
	dropped   metric.Int64Counter
	failed    metric.Int64Counter
	now       func() time.Time
}

func NewToolCallLogBridgeHandler(
	logger *slog.Logger,
	meterProvider metric.MeterProvider,
	publisher gcp.Publisher[*otelv1.InboundLogRecord],
) *ToolCallLogBridgeHandler {
	logger = logger.With(attr.SlogComponent("tool-call-log-bridge-handler"))
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/otel")
	bridged, err := meter.Int64Counter(
		meterToolCallLogBridgeBridged,
		metric.WithDescription("Tool call log records republished into the inbound OTel log pipeline"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(meterToolCallLogBridgeBridged), attr.SlogError(err))
	}
	dropped, err := meter.Int64Counter(
		meterToolCallLogBridgeDropped,
		metric.WithDescription("Tool call log records the bridge could not republish and acknowledged, by reason"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(meterToolCallLogBridgeDropped), attr.SlogError(err))
	}
	failed, err := meter.Int64Counter(
		meterToolCallLogBridgeFailed,
		metric.WithDescription("Tool call log records whose republish failed and were nacked for redelivery, by reason"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(meterToolCallLogBridgeFailed), attr.SlogError(err))
	}

	return &ToolCallLogBridgeHandler{
		logger:    logger,
		publisher: publisher,
		bridged:   bridged,
		dropped:   dropped,
		failed:    failed,
		now:       time.Now,
	}
}

var _ streams.BatchResultHandler[*telemetryv1.LogRecord] = (*ToolCallLogBridgeHandler)(nil)

// toolCallLogBridgeMessage is one mirrored row with the hook that nacks it,
// so the batch logic can be exercised without a receive batch behind it.
type toolCallLogBridgeMessage struct {
	record *telemetryv1.LogRecord
	fail   func(error)
}

func (h *ToolCallLogBridgeHandler) HandleBatchWithResult(
	ctx context.Context,
	messages []streams.BatchMessage[*telemetryv1.LogRecord],
) error {
	bridgeMessages := make([]toolCallLogBridgeMessage, len(messages))
	for i, message := range messages {
		bridgeMessages[i] = toolCallLogBridgeMessage{
			record: message.Message,
			fail:   message.Fail,
		}
	}
	return h.handleBatch(ctx, bridgeMessages)
}

func (h *ToolCallLogBridgeHandler) handleBatch(ctx context.Context, messages []toolCallLogBridgeMessage) error {
	type pending struct {
		message toolCallLogBridgeMessage
		result  gcp.PublishResult
	}

	pendings := make([]pending, 0, len(messages))
	dropped := make(map[relayReason]int)
	for _, message := range messages {
		inbound, reason, ok := bridgedToolCallLogRecord(message.record, h.now)
		if !ok {
			dropped[reason]++
			if reason != relayReasonExcluded {
				h.logger.WarnContext(ctx, "dropping tool call log record the bridge cannot republish",
					attr.SlogReason(string(reason)),
					attr.SlogTelemetryLogID(message.record.GetId()),
				)
			}
			continue
		}
		pendings = append(pendings, pending{message: message, result: h.publisher.Publish(ctx, inbound)})
	}
	for reason, count := range dropped {
		h.recordDropped(ctx, count, reason)
	}

	for _, item := range pendings {
		if _, err := item.result.Get(ctx); err != nil {
			item.message.fail(fmt.Errorf("publish bridged tool call log record: %w", err))
			h.recordFailed(ctx, 1, bridgeReasonPublishError)
			h.logger.WarnContext(ctx, "publish bridged tool call log record",
				attr.SlogError(err),
				attr.SlogTelemetryLogID(item.message.record.GetId()),
			)
			continue
		}
		h.recordBridged(ctx, 1)
	}
	return nil
}

// bridgedToolCallLogRecord turns one mirrored telemetry row into the inbound
// record the ingest edge would have published for it, or says why it cannot.
// A row that is not a tool call is excluded; a row without tenancy or with
// an unreadable payload is invalid. Neither is retried.
func bridgedToolCallLogRecord(record *telemetryv1.LogRecord, now func() time.Time) (*otelv1.InboundLogRecord, relayReason, bool) {
	key, reason, ok := toolCallLogRouteKey(record)
	if !ok {
		return nil, reason, false
	}

	// Observed time comes from the row, never from the clock when the row
	// has one, so a redelivery stamps the same value and collapses at read
	// time. The ingest edge stamps a producer's export once before its first
	// publish; the row is the bridge's equivalent of that first look.
	observedAtUnixNano := record.GetObservedTimeUnixNano()
	if observedAtUnixNano <= 0 {
		observedAtUnixNano = record.GetTimeUnixNano()
	}
	if observedAtUnixNano <= 0 {
		observedAtUnixNano = now().UnixNano()
	}
	observedAt := time.Unix(0, observedAtUnixNano).UTC()

	otlp, err := toolCallLogRecord(record, observedAt)
	if err != nil {
		return nil, relayReasonInvalid, false
	}
	otlpResource, err := toolCallLogResource(record)
	if err != nil {
		return nil, relayReasonInvalid, false
	}
	resource := &otelv1.InboundLogRecord_Resource{}
	if err := transcodeOTLPMessage(otlpResource, resource); err != nil {
		return nil, relayReasonInvalid, false
	}

	scopeName := dialect.GramTelemetryLogScope
	scope := (&otelv1.InboundLogRecord_InstrumentationScope_builder{Name: &scopeName}).Build()
	organizationID := key.organizationID
	projectID := key.projectID.String()
	provenance := (&otelv1.InboundLogRecord_Provenance_builder{
		Source:         new(ProvenanceSource),
		OrganizationId: &organizationID,
		ProjectId:      &projectID,
	}).Build()

	inbound, err := newInboundLogRecord(otlp, resource, scope, provenance, record.GetId(), func() time.Time { return observedAt })
	if err != nil {
		return nil, relayReasonInvalid, false
	}
	if err := ValidateInboundLogRecord(inbound); err != nil {
		return nil, relayReasonInvalid, false
	}
	return inbound, "", true
}

func (h *ToolCallLogBridgeHandler) recordBridged(ctx context.Context, count int) {
	if h.bridged == nil || count == 0 {
		return
	}
	h.bridged.Add(ctx, int64(count))
}

func (h *ToolCallLogBridgeHandler) recordDropped(ctx context.Context, count int, reason relayReason) {
	if h.dropped == nil || count == 0 {
		return
	}
	h.dropped.Add(ctx, int64(count), metric.WithAttributes(attr.Reason(string(reason))))
}

func (h *ToolCallLogBridgeHandler) recordFailed(ctx context.Context, count int, reason relayReason) {
	if h.failed == nil || count == 0 {
		return
	}
	h.failed.Add(ctx, int64(count), metric.WithAttributes(attr.Reason(string(reason))))
}
