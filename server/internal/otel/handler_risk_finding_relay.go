package otel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	otelattr "go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	"golang.org/x/sync/errgroup"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/dataexports"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	riskservice "github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/streams"
)

const (
	meterRiskFindingRelayDropped = "gram.otel_relay.risk_findings_dropped"
	meterRiskFindingRelayFailed  = "gram.otel_relay.risk_findings_failed"
	riskFindingEventName         = "gram.risk.finding.created"
	riskFindingScopeName         = "github.com/speakeasy-api/gram/server/internal/otel"
	riskFindingBody              = "Risk finding detected"
)

type findingExclusionEvaluator interface {
	ExcludedBy(context.Context, *riskv1.Finding) (uuid.UUID, bool, error)
}

type RiskFindingRelayHandler struct {
	logger          *slog.Logger
	findingsDropped metric.Int64Counter
	findingsFailed  metric.Int64Counter
	relay           *signalRelay
	exclusions      findingExclusionEvaluator
	now             func() time.Time
}

type riskFindingRelayMessage struct {
	finding   *riskv1.Finding
	projectID uuid.UUID
	createdAt time.Time
	fail      func(error)
}

type riskFindingRouteGroup struct {
	key      relayRouteKey
	messages []riskFindingRelayMessage
}

func NewRiskFindingRelayHandler(
	logger *slog.Logger,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	encryptionClient *encryption.Client,
	policy *guardian.Policy,
) *RiskFindingRelayHandler {
	logger = logger.With(attr.SlogComponent("risk-finding-relay-handler"))
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/otel")
	findingsDropped, err := meter.Int64Counter(
		meterRiskFindingRelayDropped,
		metric.WithDescription("Risk findings permanently omitted from the customer destination relay"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(meterRiskFindingRelayDropped), attr.SlogError(err))
	}
	findingsFailed, err := meter.Int64Counter(
		meterRiskFindingRelayFailed,
		metric.WithDescription("Risk finding delivery attempts staged for Pub/Sub retry"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(meterRiskFindingRelayFailed), attr.SlogError(err))
	}

	return &RiskFindingRelayHandler{
		logger:          logger,
		findingsDropped: findingsDropped,
		findingsFailed:  findingsFailed,
		relay: newSignalRelay(
			db,
			encryptionClient,
			policy,
			dataexports.DataSourceRiskFindings,
			"/v1/logs",
			"risk finding",
		),
		exclusions: riskservice.NewFindingExclusionResolver(db),
		now:        time.Now,
	}
}

var _ streams.BatchResultHandler[*riskv1.Finding] = (*RiskFindingRelayHandler)(nil)

func (h *RiskFindingRelayHandler) HandleBatchWithResult(
	ctx context.Context,
	messages []streams.BatchMessage[*riskv1.Finding],
) error {
	relayMessages := make([]riskFindingRelayMessage, 0, len(messages))
	dropped := make(map[relayReason]int)
	for _, message := range messages {
		relayMessage, reason, ok := newRiskFindingRelayMessage(message.Message, message.Fail)
		if !ok {
			dropped[reason]++
			continue
		}
		relayMessages = append(relayMessages, relayMessage)
	}
	for reason, count := range dropped {
		h.recordDroppedFindings(ctx, count, reason)
	}

	return h.handleBatch(ctx, relayMessages)
}

func newRiskFindingRelayMessage(finding *riskv1.Finding, fail func(error)) (riskFindingRelayMessage, relayReason, bool) {
	var emptyMessage riskFindingRelayMessage
	if finding == nil || strings.TrimSpace(finding.GetOrganizationId()) == "" {
		return emptyMessage, relayReasonInvalid, false
	}
	if finding.GetDeadLetterReason() != "" {
		return emptyMessage, relayReasonDeadLetter, false
	}

	switch finding.GetEventKind() {
	case "finding":
	case "":
		if finding.GetExcludedAt() != "" || finding.GetFalsePositiveAt() != "" {
			return emptyMessage, relayReasonStateChange, false
		}
	case "suppression", "unsuppression":
		return emptyMessage, relayReasonStateChange, false
	default:
		return emptyMessage, relayReasonInvalid, false
	}

	if _, err := uuid.Parse(finding.GetId()); err != nil {
		return emptyMessage, relayReasonInvalid, false
	}
	projectID, err := uuid.Parse(finding.GetProjectId())
	if err != nil {
		return emptyMessage, relayReasonInvalid, false
	}
	if _, parseErr := uuid.Parse(finding.GetRiskPolicyId()); parseErr != nil {
		return emptyMessage, relayReasonInvalid, false
	}
	createdAt, err := time.Parse(time.RFC3339, finding.GetCreatedAt())
	if err != nil || createdAt.Unix() < 0 {
		return emptyMessage, relayReasonInvalid, false
	}

	return riskFindingRelayMessage{
		finding:   finding,
		projectID: projectID,
		createdAt: createdAt,
		fail:      fail,
	}, "", true
}

func (h *RiskFindingRelayHandler) handleBatch(ctx context.Context, messages []riskFindingRelayMessage) error {
	groups := groupRiskFindingsByRoute(messages)
	type destinationDelivery struct {
		destination *relayDestination
		batch       rightSizedProtoBatch[riskFindingRelayMessage, *collectorlogsv1.ExportLogsServiceRequest]
	}
	deliveries := make([]destinationDelivery, 0, len(groups))

	for _, group := range groups {
		destination, err := h.relay.destinationForRoute(ctx, group.key)
		if err != nil {
			deliveryErr := fmt.Errorf("load risk finding relay destination: %w", err)
			h.failRiskFindingMessages(ctx, group.messages, deliveryErr, relayReasonConfigError)
			h.logger.ErrorContext(
				ctx,
				"load risk finding relay destination",
				attr.SlogError(err),
				attr.SlogOrganizationID(group.key.organizationID),
				attr.SlogProjectID(group.key.projectID.String()),
			)
			continue
		}
		if destination == nil {
			h.recordDroppedFindings(ctx, len(group.messages), relayReasonNoDestination)
			continue
		}

		eligible := make([]riskFindingRelayMessage, 0, len(group.messages))
		for _, message := range group.messages {
			_, excluded, lookupErr := h.exclusions.ExcludedBy(ctx, message.finding)
			if lookupErr != nil {
				exclusionErr := fmt.Errorf("evaluate risk finding exclusions: %w", lookupErr)
				h.failRiskFindingMessages(ctx, []riskFindingRelayMessage{message}, exclusionErr, relayReasonConfigError)
				h.logger.ErrorContext(
					ctx,
					"evaluate risk finding exclusions",
					attr.SlogError(lookupErr),
					attr.SlogOrganizationID(group.key.organizationID),
					attr.SlogProjectID(group.key.projectID.String()),
					attr.SlogRiskFindingID(message.finding.GetId()),
				)
				continue
			}
			if excluded {
				h.recordDroppedFindings(ctx, 1, relayReasonExcluded)
				continue
			}
			eligible = append(eligible, message)
		}
		if len(eligible) == 0 {
			continue
		}

		observedAt := h.now().UTC()
		batches, err := rightSizeProtoBatches(eligible, maxLogRelayExportBytes, func(batch []riskFindingRelayMessage) (*collectorlogsv1.ExportLogsServiceRequest, error) {
			return buildRiskFindingRelayExport(batch, observedAt), nil
		})
		if err != nil {
			h.recordDroppedFindings(ctx, len(eligible), relayReasonInvalid)
			h.logger.ErrorContext(
				ctx,
				"build risk finding relay exports",
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
					h.failRiskFindingMessages(ctx, item.batch.items, err, reason)
				} else {
					h.recordDroppedFindings(ctx, len(item.batch.items), reason)
				}
				h.logger.WarnContext(
					ctx,
					"relay risk finding OTLP logs",
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
		return o11y.LogError(ctx, h.logger, fmt.Errorf("wait for risk finding relay exports: %w", err), "failed to relay risk finding batch")
	}
	return nil
}

func (h *RiskFindingRelayHandler) failRiskFindingMessages(ctx context.Context, messages []riskFindingRelayMessage, err error, reason relayReason) {
	for _, message := range messages {
		if message.fail != nil {
			message.fail(err)
		}
	}
	h.recordFailedFindings(ctx, len(messages), reason)
}

func (h *RiskFindingRelayHandler) recordDroppedFindings(ctx context.Context, count int, reason relayReason) {
	if h.findingsDropped != nil {
		h.findingsDropped.Add(ctx, int64(count), metric.WithAttributes(attr.Reason(string(reason))))
	}
}

func (h *RiskFindingRelayHandler) recordFailedFindings(ctx context.Context, count int, reason relayReason) {
	if h.findingsFailed != nil {
		h.findingsFailed.Add(ctx, int64(count), metric.WithAttributes(attr.Reason(string(reason))))
	}
}

func groupRiskFindingsByRoute(messages []riskFindingRelayMessage) []riskFindingRouteGroup {
	indexes := make(map[relayRouteKey]int)
	groups := make([]riskFindingRouteGroup, 0)
	for _, message := range messages {
		key := relayRouteKey{
			organizationID: message.finding.GetOrganizationId(),
			projectID:      message.projectID,
		}
		index, ok := indexes[key]
		if !ok {
			index = len(groups)
			indexes[key] = index
			groups = append(groups, riskFindingRouteGroup{key: key, messages: nil})
		}
		groups[index].messages = append(groups[index].messages, message)
	}
	return groups
}

func buildRiskFindingRelayExport(messages []riskFindingRelayMessage, observedAt time.Time) *collectorlogsv1.ExportLogsServiceRequest {
	records := make([]*logsv1.LogRecord, len(messages))
	for i, message := range messages {
		records[i] = riskFindingLogRecord(message, observedAt)
	}

	return &collectorlogsv1.ExportLogsServiceRequest{
		ResourceLogs: []*logsv1.ResourceLogs{{
			Resource: &resourcev1.Resource{
				Attributes:             []*commonv1.KeyValue{riskFindingStringAttribute(string(attr.ServiceNameKey), "gram")},
				DroppedAttributesCount: 0,
				EntityRefs:             nil,
			},
			ScopeLogs: []*logsv1.ScopeLogs{{
				Scope: &commonv1.InstrumentationScope{
					Name:                   riskFindingScopeName,
					Version:                "",
					Attributes:             nil,
					DroppedAttributesCount: 0,
				},
				LogRecords: records,
			}},
		}},
	}
}

func riskFindingLogRecord(message riskFindingRelayMessage, observedAt time.Time) *logsv1.LogRecord {
	finding := message.finding
	body := strings.TrimSpace(finding.GetDescription())
	if body == "" {
		body = riskFindingBody
	}

	attributes := []*commonv1.KeyValue{
		riskFindingStringAttribute(string(attr.RiskFindingIDKey), finding.GetId()),
		riskFindingStringAttribute(string(attr.OrganizationIDKey), finding.GetOrganizationId()),
		riskFindingStringAttribute(string(attr.ProjectIDKey), finding.GetProjectId()),
		riskFindingStringAttribute(string(attr.RiskPolicyIDKey), finding.GetRiskPolicyId()),
		riskFindingIntAttribute(string(attr.RiskPolicyVersionKey), finding.GetRiskPolicyVersion()),
		riskFindingStringAttribute(string(attr.RiskRuleIDKey), finding.GetRuleId()),
		riskFindingStringAttribute(string(attr.RiskSourceKey), finding.GetSource()),
		riskFindingDoubleAttribute(string(attr.RiskConfidenceKey), finding.GetConfidence()),
		riskFindingIntAttribute(string(attr.RiskStartPosKey), int64(finding.GetStartPos())),
		riskFindingIntAttribute(string(attr.RiskEndPosKey), int64(finding.GetEndPos())),
	}
	attributes = appendOptionalRiskFindingStringAttribute(attributes, attr.RiskScanRequestIDKey, finding.GetRequestId())
	attributes = appendOptionalRiskFindingStringAttribute(attributes, attr.MessageIDKey, finding.GetChatMessageId())
	attributes = appendOptionalRiskFindingStringAttribute(attributes, attr.ChatContentPartIDKey, finding.GetContentPartId())
	attributes = appendOptionalRiskFindingStringAttribute(attributes, attr.RiskSurfaceKey, finding.GetSurface())
	attributes = appendOptionalRiskFindingStringAttribute(attributes, attr.RiskFieldKey, finding.GetField())
	attributes = appendOptionalRiskFindingStringAttribute(attributes, attr.RiskPathKey, finding.GetPath())
	attributes = appendOptionalRiskFindingStringAttribute(attributes, attr.GenAIToolCallIDKey, finding.GetToolCallId())
	if len(finding.GetTags()) > 0 {
		attributes = append(attributes, riskFindingStringSliceAttribute(string(attr.RiskTagsKey), finding.GetTags()))
	}

	return &logsv1.LogRecord{
		TimeUnixNano:           uint64(message.createdAt.UnixNano()),
		ObservedTimeUnixNano:   uint64(observedAt.UnixNano()),
		SeverityNumber:         logsv1.SeverityNumber_SEVERITY_NUMBER_WARN,
		SeverityText:           "WARN",
		Body:                   &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: body}},
		Attributes:             attributes,
		DroppedAttributesCount: 0,
		Flags:                  0,
		TraceId:                nil,
		SpanId:                 nil,
		EventName:              riskFindingEventName,
	}
}

func appendOptionalRiskFindingStringAttribute(attributes []*commonv1.KeyValue, key otelattr.Key, value string) []*commonv1.KeyValue {
	if value == "" {
		return attributes
	}
	return append(attributes, riskFindingStringAttribute(string(key), value))
}

func riskFindingStringAttribute(key, value string) *commonv1.KeyValue {
	return &commonv1.KeyValue{Key: key, Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: value}}, KeyStrindex: 0}
}

func riskFindingIntAttribute(key string, value int64) *commonv1.KeyValue {
	return &commonv1.KeyValue{Key: key, Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_IntValue{IntValue: value}}, KeyStrindex: 0}
}

func riskFindingDoubleAttribute(key string, value float64) *commonv1.KeyValue {
	return &commonv1.KeyValue{Key: key, Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_DoubleValue{DoubleValue: value}}, KeyStrindex: 0}
}

func riskFindingStringSliceAttribute(key string, values []string) *commonv1.KeyValue {
	items := make([]*commonv1.AnyValue, len(values))
	for i, value := range values {
		items[i] = &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: value}}
	}
	return &commonv1.KeyValue{
		Key:         key,
		Value:       &commonv1.AnyValue{Value: &commonv1.AnyValue_ArrayValue{ArrayValue: &commonv1.ArrayValue{Values: items}}},
		KeyStrindex: 0,
	}
}
