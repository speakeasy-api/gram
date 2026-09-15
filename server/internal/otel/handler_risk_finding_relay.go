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

type RiskFindingRelayHandler struct {
	logger          *slog.Logger
	findingsDropped metric.Int64Counter
	findingsFailed  metric.Int64Counter
	relay           *signalRelay
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
		now: time.Now,
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
		if h.findingsDropped != nil {
			h.findingsDropped.Add(ctx, int64(count), metric.WithAttributes(attr.Reason(string(reason))))
		}
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
	exclusions := riskservice.NewFindingExclusionResolver(h.relay.db)
	type destinationDelivery struct {
		destination *relayDestination
		batch       rightSizedProtoBatch[riskFindingRelayMessage, *collectorlogsv1.ExportLogsServiceRequest]
	}
	deliveries := make([]destinationDelivery, 0, len(groups))

	for _, group := range groups {
		destination, err := h.relay.destinationForRoute(ctx, group.key)
		if err != nil {
			deliveryErr := fmt.Errorf("load risk finding relay destination: %w", err)
			for _, message := range group.messages {
				if message.fail != nil {
					message.fail(deliveryErr)
				}
			}
			if h.findingsFailed != nil {
				h.findingsFailed.Add(ctx, int64(len(group.messages)), metric.WithAttributes(attr.Reason(string(relayReasonConfigError))))
			}
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
			if h.findingsDropped != nil {
				h.findingsDropped.Add(ctx, int64(len(group.messages)), metric.WithAttributes(attr.Reason(string(relayReasonNoDestination))))
			}
			continue
		}

		eligible := make([]riskFindingRelayMessage, 0, len(group.messages))
		for _, message := range group.messages {
			_, excluded, lookupErr := exclusions.ExcludedBy(ctx, message.finding)
			if lookupErr != nil {
				exclusionErr := fmt.Errorf("evaluate risk finding exclusions: %w", lookupErr)
				if message.fail != nil {
					message.fail(exclusionErr)
				}
				if h.findingsFailed != nil {
					h.findingsFailed.Add(ctx, 1, metric.WithAttributes(attr.Reason(string(relayReasonConfigError))))
				}
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
				if h.findingsDropped != nil {
					h.findingsDropped.Add(ctx, 1, metric.WithAttributes(attr.Reason(string(relayReasonExcluded))))
				}
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
			if h.findingsDropped != nil {
				h.findingsDropped.Add(ctx, int64(len(eligible)), metric.WithAttributes(attr.Reason(string(relayReasonInvalid))))
			}
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
					for _, message := range item.batch.items {
						if message.fail != nil {
							message.fail(err)
						}
					}
					if h.findingsFailed != nil {
						h.findingsFailed.Add(ctx, int64(len(item.batch.items)), metric.WithAttributes(attr.Reason(string(reason))))
					}
				} else if h.findingsDropped != nil {
					h.findingsDropped.Add(ctx, int64(len(item.batch.items)), metric.WithAttributes(attr.Reason(string(reason))))
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
				Attributes: []*commonv1.KeyValue{{
					Key:         string(attr.ServiceNameKey),
					Value:       &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "gram"}},
					KeyStrindex: 0,
				}},
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

	attributes := []*commonv1.KeyValue{
		{
			Key:         string(attr.RiskFindingIDKey),
			Value:       &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: finding.GetId()}},
			KeyStrindex: 0,
		},
		{
			Key:         string(attr.OrganizationIDKey),
			Value:       &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: finding.GetOrganizationId()}},
			KeyStrindex: 0,
		},
		{
			Key:         string(attr.ProjectIDKey),
			Value:       &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: finding.GetProjectId()}},
			KeyStrindex: 0,
		},
		{
			Key:         string(attr.RiskPolicyIDKey),
			Value:       &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: finding.GetRiskPolicyId()}},
			KeyStrindex: 0,
		},
		{
			Key:         string(attr.RiskPolicyVersionKey),
			Value:       &commonv1.AnyValue{Value: &commonv1.AnyValue_IntValue{IntValue: finding.GetRiskPolicyVersion()}},
			KeyStrindex: 0,
		},
		{
			Key:         string(attr.RiskRuleIDKey),
			Value:       &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: finding.GetRuleId()}},
			KeyStrindex: 0,
		},
		{
			Key:         string(attr.RiskSourceKey),
			Value:       &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: finding.GetSource()}},
			KeyStrindex: 0,
		},
		{
			Key:         string(attr.RiskConfidenceKey),
			Value:       &commonv1.AnyValue{Value: &commonv1.AnyValue_DoubleValue{DoubleValue: finding.GetConfidence()}},
			KeyStrindex: 0,
		},
		{
			Key:         string(attr.RiskStartPosKey),
			Value:       &commonv1.AnyValue{Value: &commonv1.AnyValue_IntValue{IntValue: int64(finding.GetStartPos())}},
			KeyStrindex: 0,
		},
		{
			Key:         string(attr.RiskEndPosKey),
			Value:       &commonv1.AnyValue{Value: &commonv1.AnyValue_IntValue{IntValue: int64(finding.GetEndPos())}},
			KeyStrindex: 0,
		},
	}
	for _, optional := range [...]struct {
		key   string
		value string
	}{
		{key: string(attr.RiskScanRequestIDKey), value: finding.GetRequestId()},
		{key: string(attr.MessageIDKey), value: finding.GetChatMessageId()},
		{key: string(attr.ChatContentPartIDKey), value: finding.GetContentPartId()},
		{key: string(attr.RiskSurfaceKey), value: finding.GetSurface()},
		{key: string(attr.RiskFieldKey), value: finding.GetField()},
		{key: string(attr.RiskPathKey), value: finding.GetPath()},
		{key: string(attr.GenAIToolCallIDKey), value: finding.GetToolCallId()},
	} {
		if optional.value == "" {
			continue
		}
		attributes = append(attributes, &commonv1.KeyValue{
			Key:         optional.key,
			Value:       &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: optional.value}},
			KeyStrindex: 0,
		})
	}
	if tags := finding.GetTags(); len(tags) > 0 {
		items := make([]*commonv1.AnyValue, len(tags))
		for i, tag := range tags {
			items[i] = &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: tag}}
		}
		attributes = append(attributes, &commonv1.KeyValue{
			Key:         string(attr.RiskTagsKey),
			Value:       &commonv1.AnyValue{Value: &commonv1.AnyValue_ArrayValue{ArrayValue: &commonv1.ArrayValue{Values: items}}},
			KeyStrindex: 0,
		})
	}

	return &logsv1.LogRecord{
		TimeUnixNano:           uint64(message.createdAt.UnixNano()),
		ObservedTimeUnixNano:   uint64(observedAt.UnixNano()),
		SeverityNumber:         logsv1.SeverityNumber_SEVERITY_NUMBER_WARN,
		SeverityText:           "WARN",
		Body:                   &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: riskFindingBody}},
		Attributes:             attributes,
		DroppedAttributesCount: 0,
		Flags:                  0,
		TraceId:                nil,
		SpanId:                 nil,
		EventName:              riskFindingEventName,
	}
}
