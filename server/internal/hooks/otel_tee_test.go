package hooks

import (
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	otelsvc "github.com/speakeasy-api/gram/server/internal/otel"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func teeTestProvenance() *otelv1.InboundLogRecord_Provenance {
	return (&otelv1.InboundLogRecord_Provenance_builder{
		Source:         new(otelsvc.ProvenanceSource),
		OrganizationId: new("org-tee-1"),
		ProjectId:      new("proj-tee-1"),
	}).Build()
}

func teeAttrByKey(t *testing.T, attrs []*otelv1.InboundLogRecord_KeyValue, key string) *otelv1.InboundLogRecord_AnyValue {
	t.Helper()
	for _, kv := range attrs {
		if kv.GetKey() == key {
			return kv.GetValue()
		}
	}
	t.Fatalf("attribute %q not found", key)
	return nil
}

func TestInboundLogRecordsFromHooksExportMapsFields(t *testing.T) {
	t.Parallel()

	timestamp := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	observed := timestamp.Add(2 * time.Second)
	now := timestamp.Add(time.Minute)
	traceID := "0af7651916cd43dd8448eb211c80319c"
	spanID := "b7ad6b7169203331"
	dropped := 3

	payload := claudeLogsPayload(
		[]*gen.OTELResourceAttribute{
			resourceStrAttr("service.name", "claude-code"),
			resourceStrAttr("host.name", "devbox.local"),
		},
		&gen.OTELScope{Name: new("claude-code"), Version: new("1.2.3")},
		&gen.OTELLogRecord{
			TimeUnixNano:           new(nanoString(timestamp)),
			ObservedTimeUnixNano:   new(nanoString(observed)),
			TraceID:                new(traceID),
			SpanID:                 new(spanID),
			Body:                   &gen.OTELLogBody{StringValue: new("api request")},
			DroppedAttributesCount: &dropped,
			Attributes: []*gen.OTELAttribute{
				strAttr("session.id", "session-1"),
				strAttr("event.name", "api_request"),
			},
		},
	)

	records := inboundLogRecordsFromHooksExport(payload, teeTestProvenance(), now)

	require.Len(t, records, 1)
	record := records[0]
	require.NoError(t, uuid.Validate(record.GetRecordId()))
	require.Equal(t, uint64(timestamp.UnixNano()), record.GetTimeUnixNano())
	require.Equal(t, uint64(observed.UnixNano()), record.GetObservedTimeUnixNano())
	require.Equal(t, traceID, hex.EncodeToString(record.GetTraceId()))
	require.Equal(t, spanID, hex.EncodeToString(record.GetSpanId()))
	require.Equal(t, "api request", record.GetBody().GetStringValue())
	require.Equal(t, uint32(3), record.GetDroppedAttributesCount())
	require.Equal(t, "session-1", teeAttrByKey(t, record.GetAttributes(), "session.id").GetStringValue())
	// The event name is promoted onto the record while the attribute stays.
	require.Equal(t, "api_request", record.GetEventName())
	require.Equal(t, "api_request", teeAttrByKey(t, record.GetAttributes(), "event.name").GetStringValue())
	require.Equal(t, "claude-code", teeAttrByKey(t, record.GetResource().GetAttributes(), "service.name").GetStringValue())
	require.Equal(t, "devbox.local", teeAttrByKey(t, record.GetResource().GetAttributes(), "host.name").GetStringValue())
	require.Equal(t, "claude-code", record.GetScope().GetName())
	require.Equal(t, "1.2.3", record.GetScope().GetVersion())
	require.Equal(t, otelsvc.ProvenanceSource, record.GetProvenance().GetSource())
	require.Equal(t, "org-tee-1", record.GetProvenance().GetOrganizationId())
	require.Equal(t, "proj-tee-1", record.GetProvenance().GetProjectId())
}

func TestInboundLogRecordsFromHooksExportStampsObservedTime(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	payload := claudeLogsPayload(
		nil,
		nil,
		&gen.OTELLogRecord{
			Body: &gen.OTELLogBody{StringValue: new("no timestamps")},
		},
	)

	records := inboundLogRecordsFromHooksExport(payload, teeTestProvenance(), now)

	require.Len(t, records, 1)
	require.Zero(t, records[0].GetTimeUnixNano())
	require.Equal(t, uint64(now.UnixNano()), records[0].GetObservedTimeUnixNano())
	require.Empty(t, records[0].GetEventName())
}

func TestInboundLogRecordsFromHooksExportDropsMalformedIDs(t *testing.T) {
	t.Parallel()

	payload := claudeLogsPayload(
		nil,
		nil,
		&gen.OTELLogRecord{
			TraceID: new("not-hex"),
			SpanID:  new("abcd"),
			Body:    &gen.OTELLogBody{StringValue: new("bad ids")},
		},
	)

	records := inboundLogRecordsFromHooksExport(payload, teeTestProvenance(), time.Now())

	require.Len(t, records, 1)
	require.Empty(t, records[0].GetTraceId())
	require.Empty(t, records[0].GetSpanId())
}

func TestInboundLogRecordsFromHooksExportConvertsLooseValues(t *testing.T) {
	t.Parallel()

	payload := claudeLogsPayload(
		nil,
		nil,
		&gen.OTELLogRecord{
			Attributes: []*gen.OTELAttribute{
				{Key: "int.string", Value: &gen.OTELAttributeValue{IntValue: "42"}},
				{Key: "double", Value: &gen.OTELAttributeValue{DoubleValue: new(1.5)}},
				{Key: "bool", Value: &gen.OTELAttributeValue{BoolValue: new(true)}},
				{Key: "bytes", Value: &gen.OTELAttributeValue{BytesValue: new("3q0=")}},
				{Key: "array", Value: &gen.OTELAttributeValue{ArrayValue: map[string]any{
					"values": []any{
						map[string]any{"stringValue": "a"},
						map[string]any{"intValue": "7"},
					},
				}}},
				{Key: "kvlist", Value: &gen.OTELAttributeValue{KvlistValue: map[string]any{
					"values": []any{
						map[string]any{"key": "nested", "value": map[string]any{"boolValue": true}},
					},
				}}},
				{Key: "array.exotic", Value: &gen.OTELAttributeValue{ArrayValue: "not-an-object"}},
			},
		},
	)

	records := inboundLogRecordsFromHooksExport(payload, teeTestProvenance(), time.Now())

	require.Len(t, records, 1)
	attrs := records[0].GetAttributes()
	require.Equal(t, int64(42), teeAttrByKey(t, attrs, "int.string").GetIntValue())
	require.InEpsilon(t, 1.5, teeAttrByKey(t, attrs, "double").GetDoubleValue(), 1e-9)
	require.True(t, teeAttrByKey(t, attrs, "bool").GetBoolValue())
	require.Equal(t, []byte{0xde, 0xad}, teeAttrByKey(t, attrs, "bytes").GetBytesValue())

	array := teeAttrByKey(t, attrs, "array").GetArrayValue().GetValues()
	require.Len(t, array, 2)
	require.Equal(t, "a", array[0].GetStringValue())
	require.Equal(t, int64(7), array[1].GetIntValue())

	kvlist := teeAttrByKey(t, attrs, "kvlist").GetKvlistValue().GetValues()
	require.Len(t, kvlist, 1)
	require.Equal(t, "nested", kvlist[0].GetKey())
	require.True(t, kvlist[0].GetValue().GetBoolValue())

	require.Equal(t, `"not-an-object"`, teeAttrByKey(t, attrs, "array.exotic").GetStringValue())
}

func TestTeeOTELLogsToEventFeedPublishesRecords(t *testing.T) {
	t.Parallel()

	var published []*otelv1.InboundLogRecord
	publisher := gcp.NewMockPublisher[*otelv1.InboundLogRecord]()
	publisher.On("Publish", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		record, ok := args.Get(1).(*otelv1.InboundLogRecord)
		require.True(t, ok)
		published = append(published, record)
	}).Return(gcp.NewSuccessPublishResult()).Twice()

	service := &Service{
		logger:           testenv.NewLogger(t),
		otelLogPublisher: publisher,
	}

	payload := claudeLogsPayload(
		[]*gen.OTELResourceAttribute{resourceStrAttr("service.name", "claude-code")},
		nil,
		&gen.OTELLogRecord{Body: &gen.OTELLogBody{StringValue: new("first")}},
		&gen.OTELLogRecord{Body: &gen.OTELLogBody{StringValue: new("second")}},
	)

	service.teeOTELLogsToEventFeed(t.Context(), payload, "org-tee-1", "proj-tee-1")
	service.otelTeeDrains.Wait()

	publisher.AssertExpectations(t)
	require.Len(t, published, 2)
	require.Equal(t, "first", published[0].GetBody().GetStringValue())
	require.Equal(t, "second", published[1].GetBody().GetStringValue())
	require.Equal(t, otelsvc.ProvenanceSource, published[0].GetProvenance().GetSource())
	require.Equal(t, "org-tee-1", published[0].GetProvenance().GetOrganizationId())
	require.Equal(t, "proj-tee-1", published[0].GetProvenance().GetProjectId())
	require.NotEqual(t, published[0].GetRecordId(), published[1].GetRecordId())
}

func TestTeeOTELLogsToEventFeedSwallowsPublishFailure(t *testing.T) {
	t.Parallel()

	publisher := gcp.NewMockPublisher[*otelv1.InboundLogRecord]()
	publisher.On("Publish", mock.Anything, mock.Anything).Return(errors.New("broker unavailable")).Once()

	service := &Service{
		logger:           testenv.NewLogger(t),
		otelLogPublisher: publisher,
	}

	payload := claudeLogsPayload(
		nil,
		nil,
		&gen.OTELLogRecord{Body: &gen.OTELLogBody{StringValue: new("doomed")}},
	)

	service.teeOTELLogsToEventFeed(t.Context(), payload, "org-tee-1", "proj-tee-1")
	service.otelTeeDrains.Wait()

	publisher.AssertExpectations(t)
}

func TestLogsTeesExportIntoEventFeedPipeline(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx := hookAuthContext(t, ctx)

	var published []*otelv1.InboundLogRecord
	publisher := gcp.NewMockPublisher[*otelv1.InboundLogRecord]()
	publisher.On("Publish", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		record, ok := args.Get(1).(*otelv1.InboundLogRecord)
		require.True(t, ok)
		published = append(published, record)
	}).Return(gcp.NewSuccessPublishResult())
	ti.service.otelLogPublisher = publisher

	timestamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	err := ti.service.Logs(ctx, claudeLogsPayload(
		[]*gen.OTELResourceAttribute{resourceStrAttr("service.name", "claude-code")},
		&gen.OTELScope{Name: new("claude-code"), Version: new("1.0.0")},
		&gen.OTELLogRecord{
			TimeUnixNano: new(nanoString(timestamp)),
			Body:         &gen.OTELLogBody{StringValue: new("teed request")},
			Attributes: []*gen.OTELAttribute{
				strAttr("session.id", "tee-session-1"),
				strAttr("event.name", "codex.conversation_starts"),
			},
		},
	))
	require.NoError(t, err)
	ti.service.otelTeeDrains.Wait()

	require.Len(t, published, 1)
	record := published[0]
	require.Equal(t, "teed request", record.GetBody().GetStringValue())
	require.Equal(t, otelsvc.ProvenanceSource, record.GetProvenance().GetSource())
	require.Equal(t, authCtx.ActiveOrganizationID, record.GetProvenance().GetOrganizationId())
	require.Equal(t, authCtx.ProjectID.String(), record.GetProvenance().GetProjectId())
	require.Equal(t, uint64(timestamp.UnixNano()), record.GetTimeUnixNano())
	require.Equal(t, "codex.conversation_starts", record.GetEventName())
	require.Equal(t, "claude-code", teeAttrByKey(t, record.GetResource().GetAttributes(), "service.name").GetStringValue())
}
