package otel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"go.opentelemetry.io/otel/metric"
	collectormetricsv1 "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	metricsv1 "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/streams"
)

const (
	meterMetricRelayMetricsDropped = "gram.otel_relay.metrics_dropped"
	meterMetricRelayMetricsFailed  = "gram.otel_relay.metrics_failed"

	metricRelayExportConcurrency = 32

	gramMetricPrivateFieldStart protowire.Number = 1000
)

type MetricRelayHandler struct {
	logger         *slog.Logger
	metricsDropped metric.Int64Counter
	metricsFailed  metric.Int64Counter
	relay          *signalRelay
}

type metricProvenanceKey struct {
	source         string
	organizationID string
	projectID      uuid.UUID
}

type metricRelayMessage struct {
	metric *otelv1.Metric
	fail   func(error)
}

type metricProvenanceGroup struct {
	key      metricProvenanceKey
	messages []metricRelayMessage
}

func NewMetricRelayHandler(
	logger *slog.Logger,
	meterProvider metric.MeterProvider,
	readReplica *pgxpool.Pool,
	encryptionClient *encryption.Client,
	policy *guardian.Policy,
) *MetricRelayHandler {
	logger = logger.With(attr.SlogComponent("metric-relay-handler"))
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/otel")
	metricsDropped, err := meter.Int64Counter(
		meterMetricRelayMetricsDropped,
		metric.WithDescription("OTEL metrics permanently dropped by the customer destination relay"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(meterMetricRelayMetricsDropped), attr.SlogError(err))
	}
	metricsFailed, err := meter.Int64Counter(
		meterMetricRelayMetricsFailed,
		metric.WithDescription("OTEL metric delivery attempts that failed and were staged for Pub/Sub retry"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(meterMetricRelayMetricsFailed), attr.SlogError(err))
	}

	return &MetricRelayHandler{
		logger:         logger,
		metricsDropped: metricsDropped,
		metricsFailed:  metricsFailed,
		relay: newSignalRelay(
			readReplica,
			encryptionClient,
			policy,
			"/v1/metrics",
			"metric",
		),
	}
}

var _ streams.BatchResultHandler[*otelv1.Metric] = (*MetricRelayHandler)(nil)

func (h *MetricRelayHandler) HandleBatchWithResult(
	ctx context.Context,
	messages []streams.BatchMessage[*otelv1.Metric],
) error {
	relayMessages := make([]metricRelayMessage, len(messages))
	for i, message := range messages {
		relayMessages[i] = metricRelayMessage{
			metric: message.Message,
			fail:   message.Fail,
		}
	}
	return h.handleBatch(ctx, relayMessages)
}

func (h *MetricRelayHandler) handleBatch(ctx context.Context, messages []metricRelayMessage) error {
	groups, invalid := groupMetricsByProvenance(messages)
	if invalid > 0 {
		h.recordDroppedMetrics(ctx, invalid, relayReasonInvalid)
	}

	type destinationResult struct {
		destination *relayDestination
		err         error
	}
	destinations := make(map[relayRouteKey]destinationResult)
	type destinationDelivery struct {
		destination *relayDestination
		batch       rightSizedProtoBatch[metricRelayMessage, *collectormetricsv1.ExportMetricsServiceRequest]
	}
	deliveries := make([]destinationDelivery, 0, len(groups))

	for _, provenanceGroup := range groups {
		routeKey := relayRouteKey{
			organizationID: provenanceGroup.key.organizationID,
			projectID:      provenanceGroup.key.projectID,
		}
		result, ok := destinations[routeKey]
		if !ok {
			result.destination, result.err = h.relay.destinationForRoute(ctx, routeKey)
			destinations[routeKey] = result
		}
		if result.err != nil {
			err := fmt.Errorf("load metric relay destination: %w", result.err)
			for _, message := range provenanceGroup.messages {
				message.fail(err)
			}
			h.recordFailedMetrics(ctx, len(provenanceGroup.messages), relayReasonConfigError)
			h.logger.ErrorContext(
				ctx,
				"load metric relay destination",
				attr.SlogError(result.err),
				attr.SlogOrganizationID(provenanceGroup.key.organizationID),
				attr.SlogProjectID(provenanceGroup.key.projectID.String()),
			)
			continue
		}
		if result.destination == nil {
			h.recordDroppedMetrics(ctx, len(provenanceGroup.messages), relayReasonNoDestination)
			continue
		}

		batches, err := rightSizeProtoBatches(provenanceGroup.messages, maxMetricRelayExportBytes, buildMetricRelayExport)
		if err != nil {
			h.recordDroppedMetrics(ctx, len(provenanceGroup.messages), relayReasonInvalid)
			h.logger.ErrorContext(
				ctx,
				"build metric relay exports",
				attr.SlogError(err),
				attr.SlogOrganizationID(provenanceGroup.key.organizationID),
				attr.SlogProjectID(provenanceGroup.key.projectID.String()),
			)
			continue
		}
		for _, batch := range batches {
			deliveries = append(deliveries, destinationDelivery{
				destination: result.destination,
				batch:       batch,
			})
		}
	}

	var exportGroup errgroup.Group
	exportGroup.SetLimit(metricRelayExportConcurrency)
	for _, item := range deliveries {
		exportGroup.Go(func() error {
			if err := item.destination.exportWithLimit(ctx, item.batch.message, maxMetricRelayExportBytes); err != nil {
				reason := relayReasonNetworkError
				retryable := true
				if exportErr, ok := errors.AsType[*relayExportError](err); ok && exportErr != nil {
					reason = exportErr.reason
					retryable = exportErr.retryable
				}

				if retryable {
					h.recordFailedMetrics(ctx, len(item.batch.items), reason)
					for _, message := range item.batch.items {
						message.fail(err)
					}
				} else {
					h.recordDroppedMetrics(ctx, len(item.batch.items), reason)
				}

				h.logger.WarnContext(
					ctx,
					"relay otel metrics",
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
		return fmt.Errorf("wait for metric relay exports: %w", err)
	}
	return nil
}

func (h *MetricRelayHandler) recordDroppedMetrics(ctx context.Context, count int, reason relayReason) {
	if h.metricsDropped == nil {
		return
	}

	h.metricsDropped.Add(ctx, int64(count), metric.WithAttributes(attr.Reason(string(reason))))
}

func (h *MetricRelayHandler) recordFailedMetrics(ctx context.Context, count int, reason relayReason) {
	if h.metricsFailed == nil {
		return
	}

	h.metricsFailed.Add(ctx, int64(count), metric.WithAttributes(attr.Reason(string(reason))))
}

func groupMetricsByProvenance(messages []metricRelayMessage) ([]metricProvenanceGroup, int) {
	indexes := make(map[metricProvenanceKey]int)
	groups := make([]metricProvenanceGroup, 0)
	invalid := 0

	for _, message := range messages {
		item := message.metric
		if item == nil {
			invalid++
			continue
		}
		provenance := item.GetProvenance()
		if provenance == nil || provenance.GetOrganizationId() == "" {
			invalid++
			continue
		}
		projectID, err := uuid.Parse(provenance.GetProjectId())
		if err != nil {
			invalid++
			continue
		}

		key := metricProvenanceKey{
			source:         provenance.GetSource(),
			organizationID: provenance.GetOrganizationId(),
			projectID:      projectID,
		}
		index, ok := indexes[key]
		if !ok {
			index = len(groups)
			indexes[key] = index
			groups = append(groups, metricProvenanceGroup{
				key:      key,
				messages: make([]metricRelayMessage, 0, 1),
			})
		}
		groups[index].messages = append(groups[index].messages, message)
	}

	return groups, invalid
}

func buildMetricRelayExport(messages []metricRelayMessage) (*collectormetricsv1.ExportMetricsServiceRequest, error) {
	metrics := make([]*otelv1.Metric, len(messages))
	for i, message := range messages {
		metrics[i] = message.metric
	}
	return newMetricRelayExportRequest(metrics)
}

func removeGramMetricFields(item *metricsv1.Metric) error {
	unknown := item.ProtoReflect().GetUnknown()
	retained := unknown[:0]
	for len(unknown) > 0 {
		fieldNumber, _, fieldLength := protowire.ConsumeField(unknown)
		if fieldLength < 0 {
			return fmt.Errorf("consume transcoded metric field: %w", protowire.ParseError(fieldLength))
		}
		if fieldNumber < gramMetricPrivateFieldStart {
			retained = append(retained, unknown[:fieldLength]...)
		}
		unknown = unknown[fieldLength:]
	}
	item.ProtoReflect().SetUnknown(retained)
	return nil
}

func newMetricRelayExportRequest(items []*otelv1.Metric) (*collectormetricsv1.ExportMetricsServiceRequest, error) {
	type scopeGroupKey struct {
		scope     string
		schemaURL string
	}
	type resourceGroupKey struct {
		resource  string
		schemaURL string
	}
	type resourceGroup struct {
		resourceMetrics *metricsv1.ResourceMetrics
		scopeIndexes    map[scopeGroupKey]int
	}

	resourceIndexes := make(map[resourceGroupKey]int)
	resourceGroups := make([]resourceGroup, 0)

	for _, item := range items {
		resourceKey := resourceGroupKey{
			resource:  "",
			schemaURL: item.GetResourceSchemaUrl(),
		}
		if resource := item.GetResource(); resource != nil {
			encoded, err := proto.Marshal(resource)
			if err != nil {
				return nil, fmt.Errorf("marshal metric resource key: %w", err)
			}
			resourceKey.resource = string(encoded)
		}

		resourceIndex, ok := resourceIndexes[resourceKey]
		if !ok {
			var resource *resourcev1.Resource
			if source := item.GetResource(); source != nil {
				resource = new(resourcev1.Resource)
				if err := transcodeOTLPMessage(source, resource); err != nil {
					return nil, fmt.Errorf("convert metric resource: %w", err)
				}
			}

			resourceIndex = len(resourceGroups)
			resourceIndexes[resourceKey] = resourceIndex
			resourceGroups = append(resourceGroups, resourceGroup{
				resourceMetrics: &metricsv1.ResourceMetrics{
					Resource:     resource,
					ScopeMetrics: nil,
					SchemaUrl:    item.GetResourceSchemaUrl(),
				},
				scopeIndexes: make(map[scopeGroupKey]int),
			})
		}

		group := &resourceGroups[resourceIndex]
		scopeKey := scopeGroupKey{
			scope:     "",
			schemaURL: item.GetScopeSchemaUrl(),
		}
		if scope := item.GetScope(); scope != nil {
			encoded, err := proto.Marshal(scope)
			if err != nil {
				return nil, fmt.Errorf("marshal metric scope key: %w", err)
			}
			scopeKey.scope = string(encoded)
		}

		scopeIndex, ok := group.scopeIndexes[scopeKey]
		if !ok {
			var scope *commonv1.InstrumentationScope
			if source := item.GetScope(); source != nil {
				scope = new(commonv1.InstrumentationScope)
				if err := transcodeOTLPMessage(source, scope); err != nil {
					return nil, fmt.Errorf("convert metric scope: %w", err)
				}
			}

			scopeIndex = len(group.resourceMetrics.ScopeMetrics)
			group.scopeIndexes[scopeKey] = scopeIndex
			group.resourceMetrics.ScopeMetrics = append(group.resourceMetrics.ScopeMetrics, &metricsv1.ScopeMetrics{
				Scope:     scope,
				Metrics:   nil,
				SchemaUrl: item.GetScopeSchemaUrl(),
			})
		}

		converted := new(metricsv1.Metric)
		if err := transcodeOTLPMessage(item, converted); err != nil {
			return nil, fmt.Errorf("convert metric: %w", err)
		}
		if err := removeGramMetricFields(converted); err != nil {
			return nil, err
		}
		scopeMetrics := group.resourceMetrics.ScopeMetrics[scopeIndex]
		scopeMetrics.Metrics = append(scopeMetrics.Metrics, converted)
	}

	request := &collectormetricsv1.ExportMetricsServiceRequest{
		ResourceMetrics: make([]*metricsv1.ResourceMetrics, len(resourceGroups)),
	}
	for i, group := range resourceGroups {
		request.ResourceMetrics[i] = group.resourceMetrics
	}
	return request, nil
}
