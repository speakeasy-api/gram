package otel

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
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
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

const (
	meterToolCallLogRelayDropped = "gram.otel_relay.tool_call_logs_dropped"
	meterToolCallLogRelayFailed  = "gram.otel_relay.tool_call_logs_failed"

	toolCallLogEventName = "gram.tool_call"
	toolCallLogScopeName = "github.com/speakeasy-api/gram/server/internal/otel"

	// maxToolCallLogAnyValueDepth bounds how deep a row's attribute JSON is
	// rebuilt into OTLP values. Tool-call attributes are flat apart from
	// header maps, so anything deeper is a producer accident, not data a
	// destination can use.
	maxToolCallLogAnyValueDepth = 8
)

// ToolCallLogRelayHandler forwards the tool-call rows Gram writes to
// telemetry_logs to the OTLP destination a project configured for the
// tool_call_logs data source.
//
// Its topic (gram.telemetry.v1.LogRecord) mirrors every telemetry_logs row —
// chat completions, agent hook events, usage measurements — so the handler
// selects the tool-call rows and silently skips the rest rather than counting
// them as drops: the other rows are simply not this relay's traffic.
//
// Rows are the same ledger entries the Observability pages read, so a
// destination sees tool calls (successes and failures alike) for hosted,
// proxied, and platform MCP servers as well as tool calls run from the
// dashboard.
type ToolCallLogRelayHandler struct {
	logger  *slog.Logger
	dropped metric.Int64Counter
	failed  metric.Int64Counter
	relay   *signalRelay
	now     func() time.Time
}

type toolCallLogRelayMessage struct {
	record     *telemetryv1.LogRecord
	key        relayRouteKey
	attributes map[string]any
	fail       func(error)
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
	dropped, err := meter.Int64Counter(
		meterToolCallLogRelayDropped,
		metric.WithDescription("Tool-call log rows permanently omitted from the customer destination relay"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(meterToolCallLogRelayDropped), attr.SlogError(err))
	}
	failed, err := meter.Int64Counter(
		meterToolCallLogRelayFailed,
		metric.WithDescription("Tool-call log delivery attempts staged for Pub/Sub retry"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(meterToolCallLogRelayFailed), attr.SlogError(err))
	}

	return &ToolCallLogRelayHandler{
		logger:  logger,
		dropped: dropped,
		failed:  failed,
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
	relayMessages := make([]toolCallLogRelayMessage, 0, len(messages))
	dropped := 0
	for _, message := range messages {
		relayMessage, eligibility := newToolCallLogRelayMessage(message.Message, message.Fail)
		switch eligibility {
		case toolCallLogEligible:
			relayMessages = append(relayMessages, relayMessage)
		case toolCallLogUnroutable:
			dropped++
		case toolCallLogOtherSource:
		}
	}
	h.recordDropped(ctx, dropped, relayReasonInvalid)

	return h.handleBatch(ctx, relayMessages)
}

// toolCallLogEligibility separates the two reasons a row does not get
// relayed. Rows from another writer are not this relay's traffic and pass
// silently; a tool-call row this relay cannot route is a real drop and gets
// counted.
type toolCallLogEligibility int

const (
	toolCallLogOtherSource toolCallLogEligibility = iota
	toolCallLogUnroutable
	toolCallLogEligible
)

// newToolCallLogRelayMessage selects the tool-call rows this relay exports and
// resolves the route their project's destination is configured on. Tenancy
// comes from the row's own columns and attributes: the project id column and
// the organization id the telemetry writer stamps on every row.
func newToolCallLogRelayMessage(record *telemetryv1.LogRecord, fail func(error)) (toolCallLogRelayMessage, toolCallLogEligibility) {
	var empty toolCallLogRelayMessage
	if record == nil {
		return empty, toolCallLogOtherSource
	}
	attributes, err := decodeTelemetryLogJSONObject(record.GetAttributesJson())
	if err != nil {
		return empty, toolCallLogOtherSource
	}
	if jsonStringValue(attributes, string(attr.EventSourceKey)) != string(telemetry.EventSourceToolCall) {
		return empty, toolCallLogOtherSource
	}

	organizationID := jsonStringValue(attributes, string(attr.OrganizationIDKey))
	if organizationID == "" {
		return empty, toolCallLogUnroutable
	}
	projectID, err := uuid.Parse(record.GetGramProjectId())
	if err != nil {
		return empty, toolCallLogUnroutable
	}
	// The row's timestamp becomes an unsigned OTLP timestamp, so a missing or
	// negative one cannot be represented.
	if record.GetTimeUnixNano() <= 0 {
		return empty, toolCallLogUnroutable
	}

	return toolCallLogRelayMessage{
		record:     record,
		key:        relayRouteKey{organizationID: organizationID, projectID: projectID},
		attributes: attributes,
		fail:       fail,
	}, toolCallLogEligible
}

func (h *ToolCallLogRelayHandler) handleBatch(ctx context.Context, messages []toolCallLogRelayMessage) error {
	groups := groupToolCallLogsByRoute(messages)
	type destinationDelivery struct {
		destination *relayDestination
		batch       rightSizedProtoBatch[toolCallLogRelayMessage, *collectorlogsv1.ExportLogsServiceRequest]
	}
	deliveries := make([]destinationDelivery, 0, len(groups))

	for _, group := range groups {
		destination, err := h.relay.destinationForRoute(ctx, group.key)
		if err != nil {
			deliveryErr := fmt.Errorf("load tool call log relay destination: %w", err)
			h.failMessages(ctx, group.messages, deliveryErr, relayReasonConfigError)
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
	if h.failed != nil {
		h.failed.Add(ctx, int64(len(messages)), metric.WithAttributes(attr.Reason(string(reason))))
	}
}

func (h *ToolCallLogRelayHandler) recordDropped(ctx context.Context, count int, reason relayReason) {
	if h.dropped == nil || count == 0 {
		return
	}
	h.dropped.Add(ctx, int64(count), metric.WithAttributes(attr.Reason(string(reason))))
}

func groupToolCallLogsByRoute(messages []toolCallLogRelayMessage) []toolCallLogRouteGroup {
	indexes := make(map[relayRouteKey]int)
	groups := make([]toolCallLogRouteGroup, 0)
	for _, message := range messages {
		index, ok := indexes[message.key]
		if !ok {
			index = len(groups)
			indexes[message.key] = index
			groups = append(groups, toolCallLogRouteGroup{key: message.key, messages: nil})
		}
		groups[index].messages = append(groups[index].messages, message)
	}
	return groups
}

// buildToolCallLogRelayExport renders one project's tool-call rows as an OTLP
// logs export, grouping records under the resource each row was written with.
// It must be deterministic: rightSizeProtoBatches re-builds overlapping
// slices and compares encoded sizes.
func buildToolCallLogRelayExport(
	messages []toolCallLogRelayMessage,
	observedAt time.Time,
	includeSensitiveData bool,
) (*collectorlogsv1.ExportLogsServiceRequest, error) {
	type resourceGroup struct {
		resourceLogs *logsv1.ResourceLogs
		records      []*logsv1.LogRecord
	}

	indexes := make(map[string]int)
	groups := make([]resourceGroup, 0, 1)
	for _, message := range messages {
		// The stored resource attributes JSON is the group key: rows written
		// by the same service and deployment share one ResourceLogs entry,
		// and byte-identical JSON is the cheapest proof of that.
		resourceKey := message.record.GetResourceAttributesJson()
		index, ok := indexes[resourceKey]
		if !ok {
			resourceAttributes, err := toolCallLogResourceAttributes(message.record)
			if err != nil {
				return nil, err
			}
			index = len(groups)
			indexes[resourceKey] = index
			groups = append(groups, resourceGroup{
				resourceLogs: &logsv1.ResourceLogs{
					Resource: &resourcev1.Resource{
						Attributes:             resourceAttributes,
						DroppedAttributesCount: 0,
						EntityRefs:             nil,
					},
					ScopeLogs: nil,
					SchemaUrl: "",
				},
				records: nil,
			})
		}
		groups[index].records = append(groups[index].records, toolCallLogRecord(message, observedAt))
	}

	request := &collectorlogsv1.ExportLogsServiceRequest{
		ResourceLogs: make([]*logsv1.ResourceLogs, len(groups)),
	}
	for i, group := range groups {
		group.resourceLogs.ScopeLogs = []*logsv1.ScopeLogs{{
			Scope: &commonv1.InstrumentationScope{
				Name:                   toolCallLogScopeName,
				Version:                "",
				Attributes:             nil,
				DroppedAttributesCount: 0,
			},
			LogRecords: group.records,
			SchemaUrl:  "",
		}}
		request.ResourceLogs[i] = group.resourceLogs
	}

	if !includeSensitiveData {
		redactSensitiveOTLP(request)
	}
	return request, nil
}

func toolCallLogRecord(message toolCallLogRelayMessage, observedAt time.Time) *logsv1.LogRecord {
	record := message.record

	observedTimeUnixNano := record.GetObservedTimeUnixNano()
	if observedTimeUnixNano <= 0 {
		observedTimeUnixNano = observedAt.UnixNano()
	}

	var body *commonv1.AnyValue
	if text := record.GetBody(); text != "" {
		body = &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: text}}
	}

	return &logsv1.LogRecord{
		TimeUnixNano:           uint64(record.GetTimeUnixNano()),
		ObservedTimeUnixNano:   uint64(observedTimeUnixNano),
		SeverityNumber:         toolCallLogSeverityNumber(record.GetSeverityText()),
		SeverityText:           record.GetSeverityText(),
		Body:                   body,
		Attributes:             toolCallLogAttributes(message),
		DroppedAttributesCount: 0,
		Flags:                  0,
		TraceId:                decodeOTLPID(record.GetTraceId(), 16),
		SpanId:                 decodeOTLPID(record.GetSpanId(), 8),
		EventName:              toolCallLogEventName,
	}
}

// toolCallLogAttributes converts the row's stored attributes back into OTLP
// key/values, in sorted key order so repeated builds encode identically. The
// row id rides along as gram.telemetry.log.id: Pub/Sub can redeliver a row,
// and it is the only stable key a destination can dedupe on.
func toolCallLogAttributes(message toolCallLogRelayMessage) []*commonv1.KeyValue {
	keys := make([]string, 0, len(message.attributes)+1)
	for key := range message.attributes {
		if _, skip := toolCallLogRedundantAttributeKeys[key]; skip {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	attributes := make([]*commonv1.KeyValue, 0, len(keys)+1)
	attributes = append(attributes, &commonv1.KeyValue{
		Key:         string(attr.TelemetryLogIDKey),
		Value:       &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: message.record.GetId()}},
		KeyStrindex: 0,
	})
	for _, key := range keys {
		attributes = append(attributes, &commonv1.KeyValue{
			Key:         key,
			Value:       toolCallLogAnyValue(message.attributes[key], 0),
			KeyStrindex: 0,
		})
	}
	return attributes
}

// toolCallLogRedundantAttributeKeys are the attributes the telemetry writer
// duplicates into the row's own columns. The OTLP record carries them as
// first-class fields, so re-sending them as attributes only inflates the
// payload.
var toolCallLogRedundantAttributeKeys = map[string]struct{}{
	string(attr.TimeUnixNanoKey):         {},
	string(attr.ObservedTimeUnixNanoKey): {},
	string(attr.LogBodyKey):              {},
}

func toolCallLogResourceAttributes(record *telemetryv1.LogRecord) ([]*commonv1.KeyValue, error) {
	values, err := decodeTelemetryLogJSONObject(record.GetResourceAttributesJson())
	if err != nil {
		return nil, fmt.Errorf("decode tool call log resource attributes: %w", err)
	}
	// Every row is written with a service name, but it lives in a column of
	// its own; seed it so a destination can always attribute the stream even
	// if the stored resource JSON is empty.
	if _, ok := values[string(attr.ServiceNameKey)]; !ok && record.GetServiceName() != "" {
		values[string(attr.ServiceNameKey)] = record.GetServiceName()
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	attributes := make([]*commonv1.KeyValue, 0, len(keys))
	for _, key := range keys {
		attributes = append(attributes, &commonv1.KeyValue{
			Key:         key,
			Value:       toolCallLogAnyValue(values[key], 0),
			KeyStrindex: 0,
		})
	}
	return attributes, nil
}

// decodeTelemetryLogJSONObject decodes one of the row's JSON object columns.
// Numbers are kept as json.Number so an integer attribute does not come back
// out of the relay as a float.
func decodeTelemetryLogJSONObject(raw string) (map[string]any, error) {
	if raw == "" {
		return map[string]any{}, nil
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(raw)))
	decoder.UseNumber()
	values := make(map[string]any)
	if err := decoder.Decode(&values); err != nil {
		return nil, fmt.Errorf("decode telemetry log JSON object: %w", err)
	}
	if values == nil {
		return map[string]any{}, nil
	}
	return values, nil
}

func jsonStringValue(values map[string]any, key string) string {
	text, _ := values[key].(string)
	return text
}

func toolCallLogAnyValue(value any, depth int) *commonv1.AnyValue {
	if depth >= maxToolCallLogAnyValueDepth {
		return &commonv1.AnyValue{Value: nil}
	}
	switch typed := value.(type) {
	case nil:
		return &commonv1.AnyValue{Value: nil}
	case string:
		return &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: typed}}
	case bool:
		return &commonv1.AnyValue{Value: &commonv1.AnyValue_BoolValue{BoolValue: typed}}
	case json.Number:
		if n, err := typed.Int64(); err == nil {
			return &commonv1.AnyValue{Value: &commonv1.AnyValue_IntValue{IntValue: n}}
		}
		if f, err := typed.Float64(); err == nil {
			return &commonv1.AnyValue{Value: &commonv1.AnyValue_DoubleValue{DoubleValue: f}}
		}
		return &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: typed.String()}}
	case []any:
		items := make([]*commonv1.AnyValue, len(typed))
		for i, item := range typed {
			items[i] = toolCallLogAnyValue(item, depth+1)
		}
		return &commonv1.AnyValue{Value: &commonv1.AnyValue_ArrayValue{ArrayValue: &commonv1.ArrayValue{Values: items}}}
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		items := make([]*commonv1.KeyValue, 0, len(keys))
		for _, key := range keys {
			items = append(items, &commonv1.KeyValue{
				Key:         key,
				Value:       toolCallLogAnyValue(typed[key], depth+1),
				KeyStrindex: 0,
			})
		}
		return &commonv1.AnyValue{Value: &commonv1.AnyValue_KvlistValue{KvlistValue: &commonv1.KeyValueList{Values: items}}}
	default:
		return &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: fmt.Sprint(typed)}}
	}
}

// toolCallLogSeverityNumber maps the severity text the telemetry writer
// derives from the tool call's response status onto the OTLP enum, so a
// destination can filter on severity without parsing the text.
func toolCallLogSeverityNumber(text string) logsv1.SeverityNumber {
	switch text {
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

// decodeOTLPID converts a hex trace or span id into the raw bytes OTLP
// carries. A malformed or wrong-length id yields no id rather than failing
// the export: correlation is a nice-to-have on these rows, delivery is not.
func decodeOTLPID(value string, size int) []byte {
	if value == "" {
		return nil
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != size {
		return nil
	}
	return decoded
}
