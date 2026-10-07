package hooks

import (
	"context"
	"encoding/json"
	"maps"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/otel/gramotel"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

const (
	// hookIngestTeeAckTimeout bounds how long a hook request waits for the
	// inbound topic to take its rows. The wait is on the request path so a
	// row the endpoint acknowledged to the agent is on the topic by then; a
	// deploy drains what is still waiting through Service.Shutdown.
	hookIngestTeeAckTimeout = 5 * time.Second

	// hookIngestFeatureLookupTimeout bounds the org feature lookups the tee
	// makes, which run detached from the request so a client disconnect
	// cannot turn an unanswered lookup into a dropped row.
	hookIngestFeatureLookupTimeout = time.Second

	hookDecisionAllow = "allow"
	hookDecisionDeny  = "deny"
)

// hookIngestRecordNamespace is the UUID namespace a hook row's record id is
// derived in. It is fixed so the same idempotency key always yields the same
// record id, and a device-spool replay that passed the idempotency gate on
// a different server still collapses at read time.
var hookIngestRecordNamespace = uuid.MustParse("3d5a2f0e-6b4c-4c1a-9e2d-7f8b1c0a5e41")

// hookTelemetryRow is one row the hooks ingest endpoint records for an
// event: the base row, and the derived skill activation when one is
// inferred. The telemetry writer stores it and the tee republishes it.
type hookTelemetryRow struct {
	timestamp time.Time
	toolName  string
	attrs     map[attr.Key]any
}

// cloneHookTelemetryRows gives the tee its own copy of each row's
// attributes, since the telemetry writer scrubs what it stores in place.
func cloneHookTelemetryRows(rows []hookTelemetryRow) []hookTelemetryRow {
	out := make([]hookTelemetryRow, len(rows))
	for i, row := range rows {
		out[i] = hookTelemetryRow{timestamp: row.timestamp, toolName: row.toolName, attrs: maps.Clone(row.attrs)}
	}
	return out
}

// teeCanonicalHookToEventFeed republishes the rows the ingest endpoint
// recorded for one hook event into the OTel pipeline, so they land in
// agent_events through the same transform as every other producer's
// records. The rows carry what the stored row carries, plus what the
// request knew and the row did not: the adapter, the raw and canonical
// event names, the permission type, Gram's verdict, the turn and the
// session, and the actor the endpoint attributed the event to.
//
// The same org gates the telemetry writer applies hold here: an org
// without logs gets nothing, and an org without tool IO logs gets the rows
// with their tool IO scrubbed. The publish acks are awaited inside the
// request, bounded, and a failure is counted and logged but never changes
// the hook's verdict: the idempotency token is already claimed, so a 5xx
// would make the agent retry a delivery the endpoint will then treat as a
// duplicate, and in a fail-closed org it would block the developer on a
// publish blip for nothing.
func (s *Service) teeCanonicalHookToEventFeed(
	ctx context.Context,
	payload *gen.IngestPayload,
	authCtx *contextvalues.AuthContext,
	metadata *SessionMetadata,
	hookSource string,
	blockReason string,
	rows []hookTelemetryRow,
) {
	if len(rows) == 0 {
		return
	}
	if s.otelLogPublisher == nil || authCtx == nil || authCtx.ProjectID == nil || !s.beginOTELTee() {
		// Nothing can be published: no publisher is wired, there is no
		// tenant to stamp, or the process is draining. Counted, so rows
		// absent from agent_events show on the publish metric.
		s.metrics.RecordEventFeedPublish(ctx, hookSource, eventFeedOutcomeSkipped, int64(len(rows)))
		return
	}
	defer s.otelTeeDrains.Done()
	orgID := authCtx.ActiveOrganizationID

	if !s.hookEventFeedFeature(ctx, orgID, productfeatures.FeatureLogs) {
		s.metrics.RecordEventFeedPublish(ctx, hookSource, eventFeedOutcomeSkipped, int64(len(rows)))
		return
	}
	scrubToolIO := !s.hookEventFeedFeature(ctx, orgID, productfeatures.FeatureToolIOLogs)

	for _, row := range rows {
		stampHookTeeAttributes(row.attrs, payload, metadata, blockReason)
		if scrubToolIO {
			telemetry.ScrubToolIO(row.attrs)
		}
		// An agent actor's self-reported identity is stripped from the stored
		// row; the copy carries exactly what the row does.
		withAgentActor(ctx, row.attrs)
	}

	provenance := (&otelv1.InboundLogRecord_Provenance_builder{
		Source:         new(gramotel.ProvenanceSource),
		OrganizationId: &orgID,
		ProjectId:      new(authCtx.ProjectID.String()),
	}).Build()
	records := inboundLogRecordsFromCanonicalHook(provenance, payload, hookSource, rows, s.now())

	// The request's cancellation must not abort the publish or the ack wait:
	// the idempotency token is already claimed, so a row dropped here would
	// never be republished.
	ctx = context.WithoutCancel(ctx)
	results := make([]gcp.PublishResult, 0, len(records))
	invalid := 0
	for _, record := range records {
		if err := gramotel.ValidateLogRecord(record); err != nil {
			invalid++
			s.logger.WarnContext(ctx, "skipping hook row the event feed pipeline would refuse",
				attr.SlogError(err),
				attr.SlogHookSource(hookSource),
				attr.SlogHookEvent(record.GetEventName()),
			)
			continue
		}
		results = append(results, s.otelLogPublisher.Publish(ctx, record))
	}
	s.metrics.RecordEventFeedPublish(ctx, hookSource, eventFeedOutcomeInvalid, int64(invalid))

	ackCtx, cancel := context.WithTimeout(ctx, hookIngestTeeAckTimeout)
	defer cancel()

	var firstErr error
	failed := 0
	for _, result := range results {
		if _, err := result.Get(ackCtx); err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if firstErr != nil {
		s.logger.ErrorContext(ctx, "publish hook rows to event feed pipeline",
			attr.SlogError(firstErr),
			attr.SlogHookSource(hookSource),
			attr.SlogTelemetryPublishFailedCount(failed),
		)
	}
	s.metrics.RecordEventFeedPublish(ctx, hookSource, eventFeedOutcomeFailure, int64(failed))
	s.metrics.RecordEventFeedPublish(ctx, hookSource, eventFeedOutcomeSuccess, int64(len(results)-failed))
}

// beginOTELTee counts a tee as in flight, or reports that Shutdown has
// started draining and the tee must not start. Taking the lock is what
// makes the Add happen before Shutdown's Wait rather than race it.
func (s *Service) beginOTELTee() bool {
	s.otelTeeMu.Lock()
	defer s.otelTeeMu.Unlock()
	if s.otelTeeDraining {
		return false
	}
	s.otelTeeDrains.Add(1)
	return true
}

// hookEventFeedFeature answers an org feature the way the telemetry writer
// does: unset client means enabled, and a lookup that fails reads as
// disabled, so the republished copy never carries more than the stored row.
func (s *Service) hookEventFeedFeature(ctx context.Context, orgID string, feature productfeatures.Feature) bool {
	if s.productFeatures == nil {
		return true
	}
	lookupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), hookIngestFeatureLookupTimeout)
	defer cancel()
	enabled, err := s.productFeatures.IsFeatureEnabled(lookupCtx, orgID, feature)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to resolve org feature for hook event feed tee",
			attr.SlogError(err),
			attr.SlogOrganizationID(orgID),
		)
		return false
	}
	return enabled
}

// stampHookTeeAttributes adds what the request knew and the stored row
// does not say: the adapter and its names for the event, the permission
// type, Gram's verdict, the turn, the agent's own session id, the actor
// the endpoint attributed the event to and the AI account it resolved. The stored row keeps the chat id
// under gen_ai.conversation.id; the raw session id is what the other
// dialects answer for the same session, so it rides as session.id.
func stampHookTeeAttributes(attrs map[attr.Key]any, payload *gen.IngestPayload, metadata *SessionMetadata, blockReason string) {
	if payload.Source != nil {
		if adapter := payload.Source.Adapter; adapter != "" {
			attrs[attr.HookAdapterKey] = adapter
		}
		if raw := conv.PtrValOr(payload.Source.RawEventName, ""); raw != "" {
			attrs[attr.HookRawEventNameKey] = raw
		}
	}
	if payload.Event != nil && payload.Event.Type != "" {
		attrs[attr.HookCanonicalEventTypeKey] = payload.Event.Type
	}
	if permission := canonicalPermissionType(payload); permission != "" {
		attrs[attr.HookPermissionTypeKey] = permission
	}
	decision := hookDecisionAllow
	if blockReason != "" {
		decision = hookDecisionDeny
	}
	attrs[attr.HookDecisionKey] = decision
	if payload.Session != nil {
		if turnID := conv.PtrValOr(payload.Session.TurnID, ""); turnID != "" {
			attrs[attr.HookTurnIDKey] = turnID
		}
	}
	if sessionID := canonicalSessionID(payload); sessionID != "" {
		attrs[attr.SessionIDKey] = sessionID
	}
	if metadata != nil {
		if metadata.UserID != "" {
			attrs[attr.UserIDKey] = metadata.UserID
		}
		if metadata.UserEmail != "" {
			attrs[attr.UserEmailKey] = metadata.UserEmail
		}
		// The AI account the session was attributed to, when the OTEL path
		// cached one: the user's id at the provider, which is what
		// agent_events means by an external user id.
		if metadata.ExternalAccountID != "" {
			attrs[attr.ExternalUserIDKey] = metadata.ExternalAccountID
		}
	}
}

// inboundLogRecordsFromCanonicalHook turns the rows recorded for one hook
// event into the inbound records the ingest edge would have published for
// them: a record id derived from the idempotency key, the row's time, the
// observed time stamped now, the resource naming the hook source, the hooks
// scope, and tenancy. Attributes are written in key order so the same rows
// always yield the same record.
func inboundLogRecordsFromCanonicalHook(
	provenance *otelv1.InboundLogRecord_Provenance,
	payload *gen.IngestPayload,
	hookSource string,
	rows []hookTelemetryRow,
	now time.Time,
) []*otelv1.InboundLogRecord {
	idempotencyKey := conv.PtrValOr(payload.IdempotencyKey, "")
	var adapterVersion *string
	if payload.Source != nil {
		if version := conv.PtrValOr(payload.Source.AdapterVersion, ""); version != "" {
			adapterVersion = &version
		}
	}
	resourceAttributes := []*otelv1.InboundLogRecord_KeyValue{
		(&otelv1.InboundLogRecord_KeyValue_builder{
			Key:   new(string(attr.ServiceNameKey)),
			Value: (&otelv1.InboundLogRecord_AnyValue_builder{StringValue: &hookSource}).Build(),
		}).Build(),
	}
	if adapterVersion != nil {
		resourceAttributes = append(resourceAttributes, (&otelv1.InboundLogRecord_KeyValue_builder{
			Key:   new(string(attr.ServiceVersionKey)),
			Value: (&otelv1.InboundLogRecord_AnyValue_builder{StringValue: adapterVersion}).Build(),
		}).Build())
	}
	resource := (&otelv1.InboundLogRecord_Resource_builder{
		Attributes:             resourceAttributes,
		DroppedAttributesCount: nil,
	}).Build()
	scope := (&otelv1.InboundLogRecord_InstrumentationScope_builder{
		Name:                   new(dialect.HooksLogScopeName),
		Version:                adapterVersion,
		Attributes:             nil,
		DroppedAttributesCount: nil,
	}).Build()
	observedNano := positiveNanoToUint64(now.UnixNano())

	records := make([]*otelv1.InboundLogRecord, 0, len(rows))
	for i, row := range rows {
		recordID := hookIngestRecordID(idempotencyKey, i)
		timeNano := positiveNanoToUint64(row.timestamp.UnixNano())
		var eventName *string
		if name, _ := row.attrs[attr.HookEventKey].(string); name != "" {
			eventName = &name
		}
		traceID, _ := row.attrs[attr.TraceIDKey].(string)
		spanID, _ := row.attrs[attr.SpanIDKey].(string)

		records = append(records, (&otelv1.InboundLogRecord_builder{
			RecordId:               &recordID,
			TimeUnixNano:           &timeNano,
			ObservedTimeUnixNano:   &observedNano,
			TraceId:                inboundEventID(&traceID, otelTeeTraceIDBytes),
			SpanId:                 inboundEventID(&spanID, otelTeeSpanIDBytes),
			Body:                   nil,
			Attributes:             inboundLogKeyValuesFromGo(row.attrs),
			DroppedAttributesCount: nil,
			SeverityNumber:         nil,
			SeverityText:           nil,
			Flags:                  nil,
			EventName:              eventName,
			Resource:               resource,
			ResourceSchemaUrl:      nil,
			Scope:                  scope,
			ScopeSchemaUrl:         nil,
			Provenance:             provenance,
		}).Build())
	}
	return records
}

// hookIngestRecordID is the record id of the index-th row recorded for a
// hook delivery: derived from the idempotency key so a replay of the same
// delivery yields the same id, and fresh when the sender stamped none.
func hookIngestRecordID(idempotencyKey string, index int) string {
	if idempotencyKey == "" {
		return uuid.NewString()
	}
	return uuid.NewSHA1(hookIngestRecordNamespace, []byte(idempotencyKey+"\x00"+strconv.Itoa(index))).String()
}

// inboundLogKeyValuesFromGo converts a row's Go-typed attributes, in key
// order, dropping only values with no representation at all.
func inboundLogKeyValuesFromGo(attrs map[attr.Key]any) []*otelv1.InboundLogRecord_KeyValue {
	keys := slices.Sorted(maps.Keys(attrs))
	out := make([]*otelv1.InboundLogRecord_KeyValue, 0, len(keys))
	for _, key := range keys {
		value := inboundAnyValueFromGo(attrs[key])
		if value == nil {
			continue
		}
		out = append(out, (&otelv1.InboundLogRecord_KeyValue_builder{
			Key:   new(string(key)),
			Value: value,
		}).Build())
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// inboundAnyValueFromGo converts one Go-typed attribute value, as the hooks
// path holds them, into its typed proto form. Anything without a scalar
// form is carried as its JSON encoding so it survives the tee.
func inboundAnyValueFromGo(value any) *otelv1.InboundLogRecord_AnyValue {
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		return (&otelv1.InboundLogRecord_AnyValue_builder{StringValue: &v}).Build()
	case bool:
		return (&otelv1.InboundLogRecord_AnyValue_builder{BoolValue: &v}).Build()
	case int:
		n := int64(v)
		return (&otelv1.InboundLogRecord_AnyValue_builder{IntValue: &n}).Build()
	case int32:
		n := int64(v)
		return (&otelv1.InboundLogRecord_AnyValue_builder{IntValue: &n}).Build()
	case int64:
		return (&otelv1.InboundLogRecord_AnyValue_builder{IntValue: &v}).Build()
	case uint32:
		n := int64(v)
		return (&otelv1.InboundLogRecord_AnyValue_builder{IntValue: &n}).Build()
	case uint64:
		if v > math.MaxInt64 {
			return jsonFallbackAnyValue(v)
		}
		n := int64(v)
		return (&otelv1.InboundLogRecord_AnyValue_builder{IntValue: &n}).Build()
	case float32:
		f := float64(v)
		return (&otelv1.InboundLogRecord_AnyValue_builder{DoubleValue: &f}).Build()
	case float64:
		return (&otelv1.InboundLogRecord_AnyValue_builder{DoubleValue: &v}).Build()
	case json.RawMessage:
		s := string(v)
		return (&otelv1.InboundLogRecord_AnyValue_builder{StringValue: &s}).Build()
	case []byte:
		s := string(v)
		return (&otelv1.InboundLogRecord_AnyValue_builder{StringValue: &s}).Build()
	default:
		return jsonFallbackAnyValue(v)
	}
}
