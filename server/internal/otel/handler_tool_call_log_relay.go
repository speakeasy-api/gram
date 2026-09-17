package otel

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	"golang.org/x/sync/errgroup"

	telemetryv1 "github.com/speakeasy-api/gram/infra/gen/gram/telemetry/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/dataexports"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/streams"
)

const (
	meterToolCallLogRelayDropped = "gram.otel_relay.tool_call_logs_dropped"
	meterToolCallLogRelayFailed  = "gram.otel_relay.tool_call_logs_failed"

	toolCallLogScopeName = "github.com/speakeasy-api/gram/server/internal/telemetry"
	toolCallLogEventName = "gram.tool_call"

	// toolCallLogEventSource is the gram.event.source value this relay
	// forwards. The topic mirrors every telemetry_logs row, and most of them
	// are agent-reported hook rows that already leave Gram through the
	// product telemetry export; forwarding those here would double-deliver
	// them.
	toolCallLogEventSource = "tool_call"
)

// ToolCallLogRelayHandler forwards the tool call records Gram writes when it
// executes a tool to the OTLP destination a project configured for the
// tool_call_logs data export.
//
// These rows never reach the product telemetry relay. They are written
// straight to ClickHouse by telemetry.Logger and mirrored onto
// gram.telemetry.v1.LogRecord, whereas product telemetry relays what arrived
// at the OTLP ingest endpoints. A tool Gram runs passes through neither, so
// without this relay a customer can watch a tool call fail in Tool Logs and
// find nothing for it in their own collector.
//
// Delivery is at-least-once: the subscription can redeliver, and a retried
// publish can duplicate a row. Every record carries its telemetry_logs id on
// gram.telemetry.log.id so a destination can dedupe.
type ToolCallLogRelayHandler struct {
	logger         *slog.Logger
	recordsDropped metric.Int64Counter
	recordsFailed  metric.Int64Counter
	relay          *signalRelay
	now            func() time.Time
}

type toolCallLogRelayMessage struct {
	record *telemetryv1.LogRecord
	fail   func(error)
}

type toolCallLogRouteGroup struct {
	key      relayRouteKey
	messages []toolCallLogRelayMessage
}

func NewToolCallLogRelayHandler(
	logger *slog.Logger,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	encryptionClient *encryption.Client,
	policy *guardian.Policy,
) *ToolCallLogRelayHandler {
	logger = logger.With(attr.SlogComponent("tool-call-log-relay-handler"))
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/otel")
	recordsDropped, err := meter.Int64Counter(
		meterToolCallLogRelayDropped,
		metric.WithDescription("Tool call log records permanently omitted from the customer destination relay"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(meterToolCallLogRelayDropped), attr.SlogError(err))
	}
	recordsFailed, err := meter.Int64Counter(
		meterToolCallLogRelayFailed,
		metric.WithDescription("Tool call log delivery attempts that failed and were staged for Pub/Sub retry"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(meterToolCallLogRelayFailed), attr.SlogError(err))
	}

	return &ToolCallLogRelayHandler{
		logger:         logger,
		recordsDropped: recordsDropped,
		recordsFailed:  recordsFailed,
		relay: newSignalRelay(
			db,
			encryptionClient,
			policy,
			dataexports.DataSourceToolCallLogs,
			"/v1/logs",
			"tool call log",
		),
		now: time.Now,
	}
}

var _ streams.BatchResultHandler[*telemetryv1.LogRecord] = (*ToolCallLogRelayHandler)(nil)

func (h *ToolCallLogRelayHandler) HandleBatchWithResult(
	ctx context.Context,
	messages []streams.BatchMessage[*telemetryv1.LogRecord],
) error {
	relayMessages := make([]toolCallLogRelayMessage, len(messages))
	for i, message := range messages {
		relayMessages[i] = toolCallLogRelayMessage{
			record: message.Message,
			fail:   message.Fail,
		}
	}
	return h.handleBatch(ctx, relayMessages)
}

func (h *ToolCallLogRelayHandler) handleBatch(ctx context.Context, messages []toolCallLogRelayMessage) error {
	groups := make([]toolCallLogRouteGroup, 0)
	indexes := make(map[relayRouteKey]int)
	dropped := make(map[relayReason]int)

	for _, message := range messages {
		key, reason, ok := toolCallLogRouteKey(message.record)
		if !ok {
			dropped[reason]++
			continue
		}

		index, seen := indexes[key]
		if !seen {
			index = len(groups)
			indexes[key] = index
			groups = append(groups, toolCallLogRouteGroup{key: key, messages: nil})
		}
		groups[index].messages = append(groups[index].messages, message)
	}

	for reason, count := range dropped {
		h.recordDropped(ctx, count, reason)
	}

	return h.handleGroups(ctx, groups)
}

// toolCallLogRouteKey decides whether a mirrored row belongs in this export
// and derives the route it belongs to. Tenancy is split across the message:
// the project is a field, while the organization is the gram.org.id attribute
// the writer stamps from ToolInfo, since telemetry_logs has no org column.
func toolCallLogRouteKey(record *telemetryv1.LogRecord) (relayRouteKey, relayReason, bool) {
	var empty relayRouteKey
	if record == nil {
		return empty, relayReasonInvalid, false
	}

	attributes, err := decodeTelemetryAttributes(record.GetAttributesJson())
	if err != nil {
		return empty, relayReasonInvalid, false
	}

	if telemetryAttributeString(attributes, string(attr.EventSourceKey)) != toolCallLogEventSource {
		return empty, relayReasonExcluded, false
	}

	organizationID := strings.TrimSpace(telemetryAttributeString(attributes, string(attr.OrganizationIDKey)))
	if organizationID == "" {
		return empty, relayReasonInvalid, false
	}
	projectID, err := uuid.Parse(record.GetGramProjectId())
	if err != nil {
		return empty, relayReasonInvalid, false
	}

	return relayRouteKey{organizationID: organizationID, projectID: projectID}, "", true
}

func (h *ToolCallLogRelayHandler) handleGroups(ctx context.Context, groups []toolCallLogRouteGroup) error {
	type destinationDelivery struct {
		destination *relayDestination
		batch       rightSizedProtoBatch[toolCallLogRelayMessage, *collectorlogsv1.ExportLogsServiceRequest]
	}
	deliveries := make([]destinationDelivery, 0, len(groups))

	for _, group := range groups {
		destination, err := h.relay.destinationForRoute(ctx, group.key)
		if err != nil {
			h.failMessages(ctx, group.messages, fmt.Errorf("load tool call log relay destination: %w", err), relayReasonConfigError)
			h.logger.ErrorContext(
				ctx,
				"load tool call log relay destination",
				attr.SlogError(err),
				attr.SlogOrganizationID(group.key.organizationID),
				attr.SlogProjectID(group.key.projectID.String()),
			)
			continue
		}
		if destination == nil {
			h.recordDropped(ctx, len(group.messages), relayReasonNoDestination)
			continue
		}

		observedAt := h.now().UTC()
		batches, err := rightSizeProtoBatches(group.messages, maxLogRelayExportBytes, func(batch []toolCallLogRelayMessage) (*collectorlogsv1.ExportLogsServiceRequest, error) {
			return buildToolCallLogRelayExport(batch, observedAt, destination.includeSensitiveData)
		})
		if err != nil {
			h.recordDropped(ctx, len(group.messages), relayReasonInvalid)
			h.logger.ErrorContext(
				ctx,
				"build tool call log relay exports",
				attr.SlogError(err),
				attr.SlogOrganizationID(group.key.organizationID),
				attr.SlogProjectID(group.key.projectID.String()),
			)
			continue
		}
		for _, batch := range batches {
			deliveries = append(deliveries, destinationDelivery{destination: destination, batch: batch})
		}
	}

	var exportGroup errgroup.Group
	exportGroup.SetLimit(logRelayExportConcurrency)
	for _, item := range deliveries {
		exportGroup.Go(func() error {
			if err := item.destination.exportWithLimit(ctx, item.batch.message, maxLogRelayExportBytes); err != nil {
				reason := relayReasonNetworkError
				retryable := true
				if exportErr, ok := errors.AsType[*relayExportError](err); ok && exportErr != nil {
					reason = exportErr.reason
					retryable = exportErr.retryable
				}

				if retryable {
					h.failMessages(ctx, item.batch.items, err, reason)
				} else {
					h.recordDropped(ctx, len(item.batch.items), reason)
				}

				h.logger.WarnContext(
					ctx,
					"relay tool call OTLP logs",
					attr.SlogError(err),
					attr.SlogOrganizationID(item.destination.organizationID),
					attr.SlogProjectID(item.destination.projectID.String()),
					attr.SlogURLFull(item.destination.endpoint),
				)
			}
			return nil
		})
	}
	if err := exportGroup.Wait(); err != nil {
		return o11y.LogError(ctx, h.logger, fmt.Errorf("wait for tool call log relay exports: %w", err), "failed to relay tool call log batch")
	}
	return nil
}

func (h *ToolCallLogRelayHandler) failMessages(ctx context.Context, messages []toolCallLogRelayMessage, err error, reason relayReason) {
	for _, message := range messages {
		if message.fail != nil {
			message.fail(err)
		}
	}
	if h.recordsFailed != nil {
		h.recordsFailed.Add(ctx, int64(len(messages)), metric.WithAttributes(attr.Reason(string(reason))))
	}
}

func (h *ToolCallLogRelayHandler) recordDropped(ctx context.Context, count int, reason relayReason) {
	if h.recordsDropped == nil || count == 0 {
		return
	}
	h.recordsDropped.Add(ctx, int64(count), metric.WithAttributes(attr.Reason(string(reason))))
}

func buildToolCallLogRelayExport(
	messages []toolCallLogRelayMessage,
	observedAt time.Time,
	includeSensitiveData bool,
) (*collectorlogsv1.ExportLogsServiceRequest, error) {
	// Resource attributes are per-row in telemetry_logs but constant across a
	// project's tool call rows (service name and version), so the first row
	// supplies the resource for the whole export rather than fragmenting it
	// into one ResourceLogs per record.
	resource, err := toolCallLogResource(messages[0].record)
	if err != nil {
		return nil, err
	}

	records := make([]*logsv1.LogRecord, len(messages))
	for i, message := range messages {
		record, err := toolCallLogRecord(message.record, observedAt)
		if err != nil {
			return nil, err
		}
		records[i] = record
	}

	request := &collectorlogsv1.ExportLogsServiceRequest{
		ResourceLogs: []*logsv1.ResourceLogs{{
			Resource: resource,
			ScopeLogs: []*logsv1.ScopeLogs{{
				Scope: &commonv1.InstrumentationScope{
					Name:                   toolCallLogScopeName,
					Version:                "",
					Attributes:             nil,
					DroppedAttributesCount: 0,
				},
				LogRecords: records,
				SchemaUrl:  "",
			}},
			SchemaUrl: "",
		}},
	}
	if !includeSensitiveData {
		redactSensitiveOTLP(request)
	}
	return request, nil
}

func toolCallLogResource(record *telemetryv1.LogRecord) (*resourcev1.Resource, error) {
	attributes, err := decodeTelemetryAttributes(record.GetResourceAttributesJson())
	if err != nil {
		return nil, fmt.Errorf("decode tool call log resource attributes: %w", err)
	}
	return &resourcev1.Resource{
		Attributes:             telemetryKeyValues(attributes),
		DroppedAttributesCount: 0,
		EntityRefs:             nil,
	}, nil
}

func toolCallLogRecord(record *telemetryv1.LogRecord, observedAt time.Time) (*logsv1.LogRecord, error) {
	attributes, err := decodeTelemetryAttributes(record.GetAttributesJson())
	if err != nil {
		return nil, fmt.Errorf("decode tool call log attributes: %w", err)
	}

	// The ClickHouse row id is the ledger's dedupe key, and this relay is
	// at-least-once, so it travels with the record for destinations that want
	// to collapse redeliveries.
	attributes[string(attr.TelemetryLogIDKey)] = record.GetId()

	observedTimeUnixNano := record.GetObservedTimeUnixNano()
	if observedTimeUnixNano <= 0 {
		observedTimeUnixNano = observedAt.UnixNano()
	}

	severityText := record.GetSeverityText()
	return &logsv1.LogRecord{
		TimeUnixNano:           uint64(max(record.GetTimeUnixNano(), 0)),
		ObservedTimeUnixNano:   uint64(max(observedTimeUnixNano, 0)),
		SeverityNumber:         toolCallLogSeverityNumber(severityText),
		SeverityText:           severityText,
		Body:                   &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: record.GetBody()}},
		Attributes:             telemetryKeyValues(attributes),
		DroppedAttributesCount: 0,
		Flags:                  0,
		TraceId:                decodeTelemetryTraceID(record.GetTraceId()),
		SpanId:                 decodeTelemetrySpanID(record.GetSpanId()),
		EventName:              toolCallLogEventName,
	}, nil
}

// toolCallLogSeverityNumber maps the row's severity text onto the OTLP enum.
// telemetry.getSeverityText only ever writes INFO, WARN, or ERROR; anything
// else is left unspecified rather than guessed at.
func toolCallLogSeverityNumber(severityText string) logsv1.SeverityNumber {
	switch strings.ToUpper(strings.TrimSpace(severityText)) {
	case "TRACE":
		return logsv1.SeverityNumber_SEVERITY_NUMBER_TRACE
	case "DEBUG":
		return logsv1.SeverityNumber_SEVERITY_NUMBER_DEBUG
	case "INFO":
		return logsv1.SeverityNumber_SEVERITY_NUMBER_INFO
	case "WARN", "WARNING":
		return logsv1.SeverityNumber_SEVERITY_NUMBER_WARN
	case "ERROR":
		return logsv1.SeverityNumber_SEVERITY_NUMBER_ERROR
	case "FATAL":
		return logsv1.SeverityNumber_SEVERITY_NUMBER_FATAL
	default:
		return logsv1.SeverityNumber_SEVERITY_NUMBER_UNSPECIFIED
	}
}

func decodeTelemetryAttributes(payload string) (map[string]any, error) {
	if strings.TrimSpace(payload) == "" {
		return make(map[string]any), nil
	}
	attributes := make(map[string]any)
	if err := json.Unmarshal([]byte(payload), &attributes); err != nil {
		return nil, fmt.Errorf("unmarshal telemetry attributes: %w", err)
	}
	return attributes, nil
}

func telemetryAttributeString(attributes map[string]any, key string) string {
	value, _ := attributes[key].(string)
	return value
}

func telemetryKeyValues(attributes map[string]any) []*commonv1.KeyValue {
	if len(attributes) == 0 {
		return nil
	}
	// Sorted so an export is byte-stable for a given row, which keeps the
	// right-sizing pass and the tests deterministic.
	keyValues := make([]*commonv1.KeyValue, 0, len(attributes))
	for _, key := range slices.Sorted(maps.Keys(attributes)) {
		keyValues = append(keyValues, &commonv1.KeyValue{
			Key:         key,
			Value:       telemetryAnyValue(attributes[key]),
			KeyStrindex: 0,
		})
	}
	return keyValues
}

// telemetryAnyValue converts one decoded JSON attribute value into OTLP.
// Unmarshalling into `any` yields only these shapes, so the default arm is
// unreachable for well-formed input and exists to keep an unexpected value
// representable rather than dropped.
func telemetryAnyValue(value any) *commonv1.AnyValue {
	switch typed := value.(type) {
	case nil:
		return &commonv1.AnyValue{Value: nil}
	case string:
		return &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: typed}}
	case bool:
		return &commonv1.AnyValue{Value: &commonv1.AnyValue_BoolValue{BoolValue: typed}}
	case float64:
		// Attributes written as Go integers round-trip through JSON as
		// float64. Emitting them as OTLP doubles would render timestamps and
		// status codes as 1.6e+18, so whole numbers go back out as integers.
		if typed == float64(int64(typed)) {
			return &commonv1.AnyValue{Value: &commonv1.AnyValue_IntValue{IntValue: int64(typed)}}
		}
		return &commonv1.AnyValue{Value: &commonv1.AnyValue_DoubleValue{DoubleValue: typed}}
	case []any:
		items := make([]*commonv1.AnyValue, len(typed))
		for i, item := range typed {
			items[i] = telemetryAnyValue(item)
		}
		return &commonv1.AnyValue{Value: &commonv1.AnyValue_ArrayValue{ArrayValue: &commonv1.ArrayValue{Values: items}}}
	case map[string]any:
		return &commonv1.AnyValue{Value: &commonv1.AnyValue_KvlistValue{KvlistValue: &commonv1.KeyValueList{Values: telemetryKeyValues(typed)}}}
	default:
		return &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: fmt.Sprint(typed)}}
	}
}

func decodeTelemetryTraceID(value string) []byte {
	return decodeTelemetryID(value, 16)
}

func decodeTelemetrySpanID(value string) []byte {
	return decodeTelemetryID(value, 8)
}

// decodeTelemetryID converts a hex trace or span id into OTLP's byte form.
// Gram stamps ids from the active span, but remote MCP synthesizes them from
// a UUID when no OTel context exists, so a wrong-length or non-hex value is
// dropped rather than failing the whole record.
func decodeTelemetryID(value string, size int) []byte {
	if len(value) != size*2 {
		return nil
	}
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return nil
	}
	return decoded
}
