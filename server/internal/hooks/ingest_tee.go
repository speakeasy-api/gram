package hooks

import (
	"context"
	"maps"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/trace"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/otelpub"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

const (
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
// event names, the permission type, Speakeasy's verdict, the turn and the
// session, and the actor the endpoint attributed the event to.
//
// The same org gates the telemetry writer applies hold here: an org
// without logs gets nothing, and an org without tool IO logs gets the rows
// with their tool IO scrubbed. The publish runs detached from the request,
// so the hook response never waits on Pub/Sub, and a failure is only
// logged.
func (s *Service) teeCanonicalHookToEventFeed(
	ctx context.Context,
	payload *gen.IngestPayload,
	authCtx *contextvalues.AuthContext,
	metadata *SessionMetadata,
	hookSource string,
	blockReason string,
	rows []hookTelemetryRow,
) {
	if len(rows) == 0 || s.otelLogPublisher == nil || authCtx == nil || authCtx.ProjectID == nil {
		return
	}
	orgID := authCtx.ActiveOrganizationID
	if !s.hookEventFeedFeature(ctx, orgID, productfeatures.FeatureLogs) {
		return
	}
	scrubToolIO := !s.hookEventFeedFeature(ctx, orgID, productfeatures.FeatureToolIOLogs)

	records := make([]log.Record, 0, len(rows))
	spans := make([]trace.SpanContext, 0, len(rows))
	for _, row := range rows {
		stampHookTeeAttributes(row.attrs, payload, metadata, blockReason)
		if scrubToolIO {
			telemetry.ScrubToolIO(row.attrs)
		}
		// An agent actor's self-reported identity is stripped from the stored
		// row; the copy carries exactly what the row does.
		withAgentActor(ctx, row.attrs)
		records = append(records, hookLogRecord(row))
		spans = append(spans, hookRowSpanContext(row.attrs))
	}

	resourceAttrs := []attribute.KeyValue{semconv.ServiceName(hookSource)}
	if payload.Source != nil {
		if version := conv.PtrValOr(payload.Source.AdapterVersion, ""); version != "" {
			resourceAttrs = append(resourceAttrs, semconv.ServiceVersion(version))
		}
	}
	logger := otelpub.NewLogger(s.otelLogPublisher, resource.NewSchemaless(resourceAttrs...), dialect.HooksLogScope)
	idempotencyKey := conv.PtrValOr(payload.IdempotencyKey, "")
	ctx = otelpub.WithTenant(context.WithoutCancel(ctx), orgID, authCtx.ProjectID.String())

	s.otelTeeDrains.Go(func() {
		for i, record := range records {
			recordCtx := otelpub.WithRecordID(trace.ContextWithSpanContext(ctx, spans[i]), hookIngestRecordID(idempotencyKey, i))
			if err := logger.Log(recordCtx, record); err != nil {
				s.logger.WarnContext(ctx, "hook row was not published to the event feed pipeline",
					attr.SlogError(err),
					attr.SlogHookSource(hookSource),
					attr.SlogHookEvent(record.EventName()),
				)
			}
		}
	})
}

// hookLogRecord is a hook row as a log record: its time, its hook event as
// the event name, and its attributes in key order.
func hookLogRecord(row hookTelemetryRow) log.Record {
	var record log.Record
	record.SetTimestamp(row.timestamp)
	if name, _ := row.attrs[attr.HookEventKey].(string); name != "" {
		record.SetEventName(name)
	}
	for _, key := range slices.Sorted(maps.Keys(row.attrs)) {
		if value, ok := hookLogValue(row.attrs[key]); ok {
			record.AddAttributes(log.KeyValue{Key: string(key), Value: value})
		}
	}
	return record
}

// hookLogValue converts one Go-typed row attribute. Anything without a
// scalar form is carried as its JSON encoding.
func hookLogValue(value any) (log.Value, bool) {
	switch v := value.(type) {
	case nil:
		return log.Value{}, false
	case string:
		return log.StringValue(v), true
	case bool:
		return log.BoolValue(v), true
	case int:
		return log.IntValue(v), true
	case int64:
		return log.Int64Value(v), true
	case float64:
		return log.Float64Value(v), true
	default:
		return log.StringValue(jsonString(v)), true
	}
}

// hookRowSpanContext is the trace and span a row names, as a remote span
// context; an id that does not parse is left out.
func hookRowSpanContext(attrs map[attr.Key]any) trace.SpanContext {
	var config trace.SpanContextConfig
	if raw, _ := attrs[attr.TraceIDKey].(string); raw != "" {
		if id, err := trace.TraceIDFromHex(raw); err == nil {
			config.TraceID = id
		}
	}
	if raw, _ := attrs[attr.SpanIDKey].(string); raw != "" {
		if id, err := trace.SpanIDFromHex(raw); err == nil {
			config.SpanID = id
		}
	}
	config.Remote = true
	return trace.NewSpanContext(config)
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
// type, Speakeasy's verdict, the turn, the agent's own session id, the actor
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

// hookIngestRecordID is the record id of the index-th row recorded for a
// hook delivery: derived from the idempotency key so a replay of the same
// delivery yields the same id, and fresh when the sender stamped none.
func hookIngestRecordID(idempotencyKey string, index int) string {
	if idempotencyKey == "" {
		return uuid.NewString()
	}
	return uuid.NewSHA1(hookIngestRecordNamespace, []byte(idempotencyKey+"\x00"+strconv.Itoa(index))).String()
}
