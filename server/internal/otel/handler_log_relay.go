package otel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"go.opentelemetry.io/otel/metric"
	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/streams"
)

const (
	meterLogRelayRecordsDropped = "gram.otel_relay.log_records_dropped"
	meterLogRelayRecordsFailed  = "gram.otel_relay.log_records_failed"

	logRelayExportConcurrency = 32
	maxLogRelayExportBytes    = 4 * constants.MiB

	gramLogPrivateFieldStart protowire.Number = 1000
)

type LogRelayHandler struct {
	logger         *slog.Logger
	recordsDropped metric.Int64Counter
	recordsFailed  metric.Int64Counter
	relay          *signalRelay
}

type logProvenanceKey struct {
	source         string
	organizationID string
	projectID      string
}

type logRelayMessage struct {
	record *otelv1.LogRecord
	fail   func(error)
}

type logProvenanceGroup struct {
	key      logProvenanceKey
	messages []logRelayMessage
}

type logRelayDelivery struct {
	request  *collectorlogsv1.ExportLogsServiceRequest
	messages []logRelayMessage
}

func NewLogRelayHandler(
	logger *slog.Logger,
	meterProvider metric.MeterProvider,
	readReplica *pgxpool.Pool,
	encryptionClient *encryption.Client,
	policy *guardian.Policy,
) *LogRelayHandler {
	logger = logger.With(attr.SlogComponent("log-relay-handler"))
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/otel")
	recordsDropped, err := meter.Int64Counter(
		meterLogRelayRecordsDropped,
		metric.WithDescription("OTEL log records permanently dropped by the customer destination relay"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(meterLogRelayRecordsDropped), attr.SlogError(err))
	}
	recordsFailed, err := meter.Int64Counter(
		meterLogRelayRecordsFailed,
		metric.WithDescription("OTEL log record delivery attempts that failed and were staged for Pub/Sub retry"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(meterLogRelayRecordsFailed), attr.SlogError(err))
	}

	return &LogRelayHandler{
		logger:         logger,
		recordsDropped: recordsDropped,
		recordsFailed:  recordsFailed,
		relay: newSignalRelay(
			readReplica,
			encryptionClient,
			policy,
			"/v1/logs",
			"log",
		),
	}
}

var _ streams.BatchResultHandler[*otelv1.LogRecord] = (*LogRelayHandler)(nil)

func (h *LogRelayHandler) HandleBatchWithResult(
	ctx context.Context,
	messages []streams.BatchMessage[*otelv1.LogRecord],
) error {
	relayMessages := make([]logRelayMessage, len(messages))
	for i, message := range messages {
		relayMessages[i] = logRelayMessage{
			record: message.Message,
			fail:   message.Fail,
		}
	}
	return h.handleBatch(ctx, relayMessages)
}

func (h *LogRelayHandler) handleBatch(ctx context.Context, messages []logRelayMessage) error {
	logger := h.logger
	groups, invalid := groupLogsByProvenance(messages)
	if invalid > 0 {
		h.recordDroppedLogs(ctx, invalid, relayReasonInvalid)
	}

	type destinationResult struct {
		destination *relayDestination
		err         error
	}
	destinations := make(map[string]destinationResult)
	type destinationDelivery struct {
		destination *relayDestination
		delivery    logRelayDelivery
	}
	deliveries := make([]destinationDelivery, 0, len(groups))

	for _, provenanceGroup := range groups {
		result, ok := destinations[provenanceGroup.key.organizationID]
		if !ok {
			result.destination, result.err = h.relay.destinationForOrganization(ctx, provenanceGroup.key.organizationID)
			destinations[provenanceGroup.key.organizationID] = result
		}
		if result.err != nil {
			err := fmt.Errorf("load log relay destination: %w", result.err)
			for _, message := range provenanceGroup.messages {
				message.fail(err)
			}
			h.recordFailedLogs(ctx, len(provenanceGroup.messages), relayReasonConfigError)
			logger.ErrorContext(
				ctx,
				"load log relay destination",
				attr.SlogError(result.err),
				attr.SlogOrganizationID(provenanceGroup.key.organizationID),
			)
			continue
		}
		if result.destination == nil {
			h.recordDroppedLogs(ctx, len(provenanceGroup.messages), relayReasonNoDestination)
			continue
		}

		groupDeliveries, dropped := splitLogRelayMessages(provenanceGroup.messages)
		if len(dropped) > 0 {
			h.recordDroppedLogs(ctx, len(dropped), relayReasonInvalid)
		}
		for _, delivery := range groupDeliveries {
			deliveries = append(deliveries, destinationDelivery{
				destination: result.destination,
				delivery:    delivery,
			})
		}
	}

	var exportGroup errgroup.Group
	exportGroup.SetLimit(logRelayExportConcurrency)
	for _, item := range deliveries {
		exportGroup.Go(func() error {
			if err := item.destination.exportWithLimit(ctx, item.delivery.request, maxLogRelayExportBytes); err != nil {
				reason := relayReasonNetworkError
				retryable := true
				if exportErr, ok := errors.AsType[*relayExportError](err); ok && exportErr != nil {
					reason = exportErr.reason
					retryable = exportErr.retryable
				}

				if retryable {
					h.recordFailedLogs(ctx, len(item.delivery.messages), reason)
					for _, message := range item.delivery.messages {
						message.fail(err)
					}
				} else {
					h.recordDroppedLogs(ctx, len(item.delivery.messages), reason)
				}

				logger.ErrorContext(
					ctx,
					"relay otel logs",
					attr.SlogError(err),
					attr.SlogOrganizationID(item.destination.organizationID),
					attr.SlogURLFull(item.destination.endpoint),
				)
			}
			return nil
		})
	}
	if err := exportGroup.Wait(); err != nil {
		return o11y.LogError(ctx, logger, fmt.Errorf("wait for log relay exports: %w", err), "failed to relay log batch")
	}
	return nil
}

func (h *LogRelayHandler) recordDroppedLogs(ctx context.Context, count int, reason relayReason) {
	if h.recordsDropped == nil {
		return
	}

	h.recordsDropped.Add(ctx, int64(count), metric.WithAttributes(attr.Reason(string(reason))))
}

func (h *LogRelayHandler) recordFailedLogs(ctx context.Context, count int, reason relayReason) {
	if h.recordsFailed == nil {
		return
	}

	h.recordsFailed.Add(ctx, int64(count), metric.WithAttributes(attr.Reason(string(reason))))
}

func groupLogsByProvenance(messages []logRelayMessage) ([]logProvenanceGroup, int) {
	indexes := make(map[logProvenanceKey]int)
	groups := make([]logProvenanceGroup, 0)
	invalid := 0

	for _, message := range messages {
		record := message.record
		if record == nil {
			invalid++
			continue
		}
		provenance := record.GetProvenance()
		if provenance == nil || provenance.GetOrganizationId() == "" {
			invalid++
			continue
		}

		key := logProvenanceKey{
			source:         provenance.GetSource(),
			organizationID: provenance.GetOrganizationId(),
			projectID:      provenance.GetProjectId(),
		}
		index, ok := indexes[key]
		if !ok {
			index = len(groups)
			indexes[key] = index
			groups = append(groups, logProvenanceGroup{
				key:      key,
				messages: make([]logRelayMessage, 0, 1),
			})
		}
		groups[index].messages = append(groups[index].messages, message)
	}

	return groups, invalid
}

func splitLogRelayMessages(messages []logRelayMessage) (deliveries []logRelayDelivery, dropped []logRelayMessage) {
	if len(messages) == 0 {
		return nil, nil
	}

	records := make([]*otelv1.LogRecord, len(messages))
	for i, message := range messages {
		records[i] = message.record
	}
	request, err := newLogRelayExportRequest(records)
	if err == nil && proto.Size(request) <= maxLogRelayExportBytes {
		return []logRelayDelivery{{request: request, messages: messages}}, nil
	}
	if len(messages) == 1 {
		return nil, messages
	}

	middle := len(messages) / 2
	leftDeliveries, leftDropped := splitLogRelayMessages(messages[:middle])
	rightDeliveries, rightDropped := splitLogRelayMessages(messages[middle:])
	return append(leftDeliveries, rightDeliveries...), append(leftDropped, rightDropped...)
}

func removeGramLogFields(record *logsv1.LogRecord) error {
	unknown := record.ProtoReflect().GetUnknown()
	retained := unknown[:0]
	for len(unknown) > 0 {
		fieldNumber, _, fieldLength := protowire.ConsumeField(unknown)
		if fieldLength < 0 {
			return fmt.Errorf("consume transcoded log record field: %w", protowire.ParseError(fieldLength))
		}
		if fieldNumber < gramLogPrivateFieldStart {
			retained = append(retained, unknown[:fieldLength]...)
		}
		unknown = unknown[fieldLength:]
	}
	record.ProtoReflect().SetUnknown(retained)
	return nil
}

func newLogRelayExportRequest(records []*otelv1.LogRecord) (*collectorlogsv1.ExportLogsServiceRequest, error) {
	type scopeGroupKey struct {
		scope     string
		schemaURL string
	}
	type resourceGroupKey struct {
		resource  string
		schemaURL string
	}
	type resourceGroup struct {
		resourceLogs *logsv1.ResourceLogs
		scopeIndexes map[scopeGroupKey]int
	}

	resourceIndexes := make(map[resourceGroupKey]int)
	resourceGroups := make([]resourceGroup, 0)

	for _, record := range records {
		resourceKey := resourceGroupKey{
			resource:  "",
			schemaURL: record.GetResourceSchemaUrl(),
		}
		if resource := record.GetResource(); resource != nil {
			encoded, err := proto.Marshal(resource)
			if err != nil {
				return nil, fmt.Errorf("marshal log resource key: %w", err)
			}
			resourceKey.resource = string(encoded)
		}

		resourceIndex, ok := resourceIndexes[resourceKey]
		if !ok {
			var resource *resourcev1.Resource
			if source := record.GetResource(); source != nil {
				resource = new(resourcev1.Resource)
				if err := transcodeOTLPMessage(source, resource); err != nil {
					return nil, fmt.Errorf("convert log resource: %w", err)
				}
			}

			resourceIndex = len(resourceGroups)
			resourceIndexes[resourceKey] = resourceIndex
			resourceGroups = append(resourceGroups, resourceGroup{
				resourceLogs: &logsv1.ResourceLogs{
					Resource:  resource,
					ScopeLogs: nil,
					SchemaUrl: record.GetResourceSchemaUrl(),
				},
				scopeIndexes: make(map[scopeGroupKey]int),
			})
		}

		group := &resourceGroups[resourceIndex]
		scopeKey := scopeGroupKey{
			scope:     "",
			schemaURL: record.GetScopeSchemaUrl(),
		}
		if scope := record.GetScope(); scope != nil {
			encoded, err := proto.Marshal(scope)
			if err != nil {
				return nil, fmt.Errorf("marshal log scope key: %w", err)
			}
			scopeKey.scope = string(encoded)
		}

		scopeIndex, ok := group.scopeIndexes[scopeKey]
		if !ok {
			var scope *commonv1.InstrumentationScope
			if source := record.GetScope(); source != nil {
				scope = new(commonv1.InstrumentationScope)
				if err := transcodeOTLPMessage(source, scope); err != nil {
					return nil, fmt.Errorf("convert log scope: %w", err)
				}
			}

			scopeIndex = len(group.resourceLogs.ScopeLogs)
			group.scopeIndexes[scopeKey] = scopeIndex
			group.resourceLogs.ScopeLogs = append(group.resourceLogs.ScopeLogs, &logsv1.ScopeLogs{
				Scope:      scope,
				LogRecords: nil,
				SchemaUrl:  record.GetScopeSchemaUrl(),
			})
		}

		converted := new(logsv1.LogRecord)
		if err := transcodeOTLPMessage(record, converted); err != nil {
			return nil, fmt.Errorf("convert log record: %w", err)
		}
		if err := removeGramLogFields(converted); err != nil {
			return nil, err
		}
		scopeLogs := group.resourceLogs.ScopeLogs[scopeIndex]
		scopeLogs.LogRecords = append(scopeLogs.LogRecords, converted)
	}

	request := &collectorlogsv1.ExportLogsServiceRequest{
		ResourceLogs: make([]*logsv1.ResourceLogs, len(resourceGroups)),
	}
	for i, group := range resourceGroups {
		request.ResourceLogs[i] = group.resourceLogs
	}
	return request, nil
}
