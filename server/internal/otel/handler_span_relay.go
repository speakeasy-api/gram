package otel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"go.opentelemetry.io/otel/metric"
	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/streams"
)

const (
	meterSpanRelaySpansDropped = "gram.otel_relay.spans_dropped"
	meterSpanRelaySpansFailed  = "gram.otel_relay.spans_failed"

	spanRelayExportConcurrency = 32

	gramSpanPrivateFieldStart protowire.Number = 1000
)

type SpanRelayHandler struct {
	logger       *slog.Logger
	spansDropped metric.Int64Counter
	spansFailed  metric.Int64Counter
	relay        *signalRelay
}

type spanProvenanceKey struct {
	source           string
	organizationID   string
	projectID        string
	organizationSlug string
	projectSlug      string
	apiKeyID         string
	apiKeyName       string
}

type spanRelayMessage struct {
	span *otelv1.Span
	fail func(error)
}

type spanProvenanceGroup struct {
	key      spanProvenanceKey
	messages []spanRelayMessage
}

func NewSpanRelayHandler(
	logger *slog.Logger,
	meterProvider metric.MeterProvider,
	readReplica *pgxpool.Pool,
	encryptionClient *encryption.Client,
	policy *guardian.Policy,
) *SpanRelayHandler {
	logger = logger.With(attr.SlogComponent("span-relay-handler"))
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/otel")
	spansDropped, err := meter.Int64Counter(
		meterSpanRelaySpansDropped,
		metric.WithDescription("OTEL spans permanently dropped by the customer destination relay"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(meterSpanRelaySpansDropped), attr.SlogError(err))
	}
	spansFailed, err := meter.Int64Counter(
		meterSpanRelaySpansFailed,
		metric.WithDescription("OTEL span delivery attempts that failed and were staged for Pub/Sub retry"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(meterSpanRelaySpansFailed), attr.SlogError(err))
	}

	return &SpanRelayHandler{
		logger:       logger,
		spansDropped: spansDropped,
		spansFailed:  spansFailed,
		relay: newSignalRelay(
			readReplica,
			encryptionClient,
			policy,
			"/v1/traces",
			"trace",
		),
	}
}

var _ streams.BatchResultHandler[*otelv1.Span] = (*SpanRelayHandler)(nil)

func (h *SpanRelayHandler) HandleBatchWithResult(
	ctx context.Context,
	messages []streams.BatchMessage[*otelv1.Span],
) error {
	relayMessages := make([]spanRelayMessage, len(messages))
	for i, message := range messages {
		relayMessages[i] = spanRelayMessage{
			span: message.Message,
			fail: message.Fail,
		}
	}
	return h.handleBatch(ctx, relayMessages)
}

func (h *SpanRelayHandler) handleBatch(ctx context.Context, messages []spanRelayMessage) error {
	logger := h.logger
	groups, invalid := groupSpansByProvenance(messages)
	if invalid > 0 {
		h.recordDroppedSpans(ctx, invalid, relayReasonInvalid)
	}

	type destinationResult struct {
		destination *relayDestination
		err         error
	}
	destinations := make(map[string]destinationResult)
	type delivery struct {
		destination *relayDestination
		request     *collectortracev1.ExportTraceServiceRequest
		messages    []spanRelayMessage
	}
	deliveries := make([]delivery, 0, len(groups))

	for _, provenanceGroup := range groups {
		result, ok := destinations[provenanceGroup.key.organizationID]
		if !ok {
			result.destination, result.err = h.relay.destinationForOrganization(ctx, provenanceGroup.key.organizationID)
			destinations[provenanceGroup.key.organizationID] = result
		}
		if result.err != nil {
			err := fmt.Errorf("load span relay destination: %w", result.err)
			for _, message := range provenanceGroup.messages {
				message.fail(err)
			}
			h.recordFailedSpans(ctx, len(provenanceGroup.messages), relayReasonConfigError)
			logger.ErrorContext(
				ctx,
				"load span relay destination",
				attr.SlogError(result.err),
				attr.SlogOrganizationID(provenanceGroup.key.organizationID),
			)
			continue
		}
		if result.destination == nil {
			h.recordDroppedSpans(ctx, len(provenanceGroup.messages), relayReasonNoDestination)
			continue
		}

		spans := make([]*otelv1.Span, len(provenanceGroup.messages))
		for i, message := range provenanceGroup.messages {
			spans[i] = message.span
		}
		request, err := newRelayExportRequest(spans)
		if err != nil {
			h.recordDroppedSpans(ctx, len(provenanceGroup.messages), relayReasonInvalid)
			continue
		}
		deliveries = append(deliveries, delivery{
			destination: result.destination,
			request:     request,
			messages:    provenanceGroup.messages,
		})
	}

	var exportGroup errgroup.Group
	exportGroup.SetLimit(spanRelayExportConcurrency)
	for _, item := range deliveries {
		exportGroup.Go(func() error {
			if err := item.destination.export(ctx, item.request); err != nil {
				reason := relayReasonNetworkError
				retryable := true
				if exportErr, ok := errors.AsType[*relayExportError](err); ok && exportErr != nil {
					reason = exportErr.reason
					retryable = exportErr.retryable
				}

				if retryable {
					h.recordFailedSpans(ctx, len(item.messages), reason)
					for _, message := range item.messages {
						message.fail(err)
					}
				} else {
					h.recordDroppedSpans(ctx, len(item.messages), reason)
				}

				logger.ErrorContext(
					ctx,
					"relay otel spans",
					attr.SlogError(err),
					attr.SlogOrganizationID(item.destination.organizationID),
					attr.SlogURLFull(item.destination.endpoint),
				)
			}
			return nil
		})
	}
	if err := exportGroup.Wait(); err != nil {
		return o11y.LogError(ctx, logger, fmt.Errorf("wait for span relay exports: %w", err), "failed to relay span batch")
	}
	return nil
}

func (h *SpanRelayHandler) recordDroppedSpans(ctx context.Context, count int, reason relayReason) {
	if h.spansDropped == nil {
		return
	}

	h.spansDropped.Add(ctx, int64(count), metric.WithAttributes(attr.Reason(string(reason))))
}

func (h *SpanRelayHandler) recordFailedSpans(ctx context.Context, count int, reason relayReason) {
	if h.spansFailed == nil {
		return
	}

	h.spansFailed.Add(ctx, int64(count), metric.WithAttributes(attr.Reason(string(reason))))
}

func groupSpansByProvenance(messages []spanRelayMessage) ([]spanProvenanceGroup, int) {
	indexes := make(map[spanProvenanceKey]int)
	groups := make([]spanProvenanceGroup, 0)
	invalid := 0

	for _, message := range messages {
		span := message.span
		if span == nil {
			invalid++
			continue
		}
		provenance := span.GetProvenance()
		if provenance == nil || provenance.GetOrganizationId() == "" {
			invalid++
			continue
		}

		key := spanProvenanceKey{
			source:           provenance.GetSource(),
			organizationID:   provenance.GetOrganizationId(),
			projectID:        provenance.GetProjectId(),
			organizationSlug: provenance.GetOrganizationSlug(),
			projectSlug:      provenance.GetProjectSlug(),
			apiKeyID:         provenance.GetApiKeyId(),
			apiKeyName:       provenance.GetApiKeyName(),
		}
		index, ok := indexes[key]
		if !ok {
			index = len(groups)
			indexes[key] = index
			groups = append(groups, spanProvenanceGroup{
				key:      key,
				messages: make([]spanRelayMessage, 0, 1),
			})
		}
		groups[index].messages = append(groups[index].messages, message)
	}

	return groups, invalid
}

func removeGramSpanFields(span *tracev1.Span) error {
	unknown := span.ProtoReflect().GetUnknown()
	retained := unknown[:0]
	for len(unknown) > 0 {
		fieldNumber, _, fieldLength := protowire.ConsumeField(unknown)
		if fieldLength < 0 {
			return fmt.Errorf("consume transcoded span field: %w", protowire.ParseError(fieldLength))
		}
		if fieldNumber < gramSpanPrivateFieldStart {
			retained = append(retained, unknown[:fieldLength]...)
		}
		unknown = unknown[fieldLength:]
	}
	span.ProtoReflect().SetUnknown(retained)
	return nil
}

func newRelayExportRequest(spans []*otelv1.Span) (*collectortracev1.ExportTraceServiceRequest, error) {
	type scopeGroupKey struct {
		scope     string
		schemaURL string
	}
	type resourceGroupKey struct {
		resource  string
		schemaURL string
	}
	type resourceGroup struct {
		resourceSpans *tracev1.ResourceSpans
		scopeIndexes  map[scopeGroupKey]int
	}

	resourceIndexes := make(map[resourceGroupKey]int)
	resourceGroups := make([]resourceGroup, 0)

	for _, span := range spans {
		resourceKey := resourceGroupKey{
			resource:  "",
			schemaURL: span.GetResourceSchemaUrl(),
		}
		if resource := span.GetResource(); resource != nil {
			encoded, err := proto.Marshal(resource)
			if err != nil {
				return nil, fmt.Errorf("marshal span resource key: %w", err)
			}
			resourceKey.resource = string(encoded)
		}

		resourceIndex, ok := resourceIndexes[resourceKey]
		if !ok {
			var resource *resourcev1.Resource
			if source := span.GetResource(); source != nil {
				resource = new(resourcev1.Resource)
				if err := transcodeOTLPMessage(source, resource); err != nil {
					return nil, fmt.Errorf("convert span resource: %w", err)
				}
			}

			resourceIndex = len(resourceGroups)
			resourceIndexes[resourceKey] = resourceIndex
			resourceGroups = append(resourceGroups, resourceGroup{
				resourceSpans: &tracev1.ResourceSpans{
					Resource:   resource,
					ScopeSpans: nil,
					SchemaUrl:  span.GetResourceSchemaUrl(),
				},
				scopeIndexes: make(map[scopeGroupKey]int),
			})
		}

		group := &resourceGroups[resourceIndex]
		scopeKey := scopeGroupKey{
			scope:     "",
			schemaURL: span.GetScopeSchemaUrl(),
		}
		if scope := span.GetScope(); scope != nil {
			encoded, err := proto.Marshal(scope)
			if err != nil {
				return nil, fmt.Errorf("marshal span scope key: %w", err)
			}
			scopeKey.scope = string(encoded)
		}

		scopeIndex, ok := group.scopeIndexes[scopeKey]
		if !ok {
			var scope *commonv1.InstrumentationScope
			if source := span.GetScope(); source != nil {
				scope = new(commonv1.InstrumentationScope)
				if err := transcodeOTLPMessage(source, scope); err != nil {
					return nil, fmt.Errorf("convert span scope: %w", err)
				}
			}

			scopeIndex = len(group.resourceSpans.ScopeSpans)
			group.scopeIndexes[scopeKey] = scopeIndex
			group.resourceSpans.ScopeSpans = append(group.resourceSpans.ScopeSpans, &tracev1.ScopeSpans{
				Scope:     scope,
				Spans:     nil,
				SchemaUrl: span.GetScopeSchemaUrl(),
			})
		}

		converted := new(tracev1.Span)
		if err := transcodeOTLPMessage(span, converted); err != nil {
			return nil, fmt.Errorf("convert span: %w", err)
		}
		// Resource and scope are rebuilt above. Strip Gram's private extension
		// range while retaining unknown OTLP fields for wire compatibility.
		if err := removeGramSpanFields(converted); err != nil {
			return nil, err
		}
		scopeSpans := group.resourceSpans.ScopeSpans[scopeIndex]
		scopeSpans.Spans = append(scopeSpans.Spans, converted)
	}

	request := &collectortracev1.ExportTraceServiceRequest{
		ResourceSpans: make([]*tracev1.ResourceSpans, len(resourceGroups)),
	}
	for i, group := range resourceGroups {
		request.ResourceSpans[i] = group.resourceSpans
	}
	return request, nil
}
