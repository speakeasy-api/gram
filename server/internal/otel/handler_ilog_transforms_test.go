package otel

import (
	"strings"
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	otelattr "go.opentelemetry.io/otel/attribute"
	"google.golang.org/protobuf/proto"
)

func TestLogTransformHandlerNormalizesEnrichesAndPublishes(t *testing.T) {
	t.Parallel()

	inbound := (&otelv1.InboundLogRecord_builder{
		RecordId: new("record-id"),
		Body:     (&otelv1.InboundLogRecord_AnyValue_builder{StringValue: new("inference details")}).Build(),
		Scope: (&otelv1.InboundLogRecord_InstrumentationScope_builder{
			Name: new("producer.scope"),
		}).Build(),
		Provenance: (&otelv1.InboundLogRecord_Provenance_builder{
			Source:         new("speakeasy"),
			OrganizationId: new(testLogOrganizationID),
			ProjectId:      new(testLogProjectID),
		}).Build(),
		Attributes: []*otelv1.InboundLogRecord_KeyValue{
			logStringAttribute("gen_ai.input.messages", `[{"role":"user","parts":[{"type":"text","content":"hello"}]}]`),
		},
	}).Build()

	var published *otelv1.LogRecord
	publisher := gcp.NewMockPublisher[*otelv1.LogRecord]()
	publisher.On("Publish", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		record, ok := args.Get(1).(*otelv1.LogRecord)
		require.True(t, ok)
		published = record
	}).Return(gcp.NewSuccessPublishResult()).Once()
	handler := NewLogTransformHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), publisher, newTestDatabase(t), cache.NoopCache)

	err := handler.Handle(t.Context(), inbound, gcp.MessageMetadata{})

	require.NoError(t, err)
	publisher.AssertExpectations(t)
	require.NotNil(t, published)
	require.Equal(t, "record-id", published.GetRecordId())
	require.Equal(t, "inference details", published.GetBody().GetStringValue())
	require.Equal(t, normalizedLogInstrumentationScopeName, published.GetScope().GetName())

	attributes := make(map[string]*otelv1.LogRecord_AnyValue, len(published.GetAttributes()))
	for _, item := range published.GetAttributes() {
		attributes[item.GetKey()] = item.GetValue()
	}
	require.Equal(t, "producer.scope", attributes[string(OriginalInstrumentationScopeNameKey)].GetStringValue())
	require.Equal(t, testLogOrganizationID, attributes[string(OrganizationIDKey)].GetStringValue())
	require.Equal(t, testLogProjectID, attributes[string(ProjectIDKey)].GetStringValue())
	require.Positive(t, attributes[string(TokensCountKey)].GetIntValue())
	require.NotEmpty(t, attributes[string(TokensCodecKey)].GetStringValue())
	require.Contains(t, attributes, "gen_ai.input.messages")
}

func TestLogAnyValueConvertsHeterogeneousSlice(t *testing.T) {
	t.Parallel()

	value := otelattr.SliceValue(
		otelattr.StringValue("text"),
		otelattr.Int64Value(42),
		otelattr.SliceValue(
			otelattr.BoolValue(true),
			otelattr.ByteSliceValue([]byte{0xde, 0xad}),
		),
	)

	converted, err := logAnyValue(value)

	require.NoError(t, err)
	values := converted.GetArrayValue().GetValues()
	require.Len(t, values, 3)
	require.Equal(t, "text", values[0].GetStringValue())
	require.Equal(t, int64(42), values[1].GetIntValue())
	nested := values[2].GetArrayValue().GetValues()
	require.Len(t, nested, 2)
	require.True(t, nested[0].GetBoolValue())
	require.Equal(t, []byte{0xde, 0xad}, nested[1].GetBytesValue())
}

func TestLogAnyValuePreservesEmptyValue(t *testing.T) {
	t.Parallel()

	converted, err := logAnyValue(otelattr.Value{})

	require.NoError(t, err)
	require.Equal(t, otelv1.LogRecord_AnyValue_Value_not_set_case, converted.WhichValue())
}

func TestMaxSizeLogRecordFitsRelayExportAfterFullEnrichment(t *testing.T) {
	t.Parallel()

	originalScopeName := strings.Repeat("producer.scope.", 1024)
	resourceSchemaURL := "https://opentelemetry.io/schemas/1.27.0"
	scopeSchemaURL := "https://opentelemetry.io/schemas/1.28.0"
	inbound := (&otelv1.InboundLogRecord_builder{
		RecordId: new("record-id"),
		Body:     (&otelv1.InboundLogRecord_AnyValue_builder{BytesValue: []byte{}}).Build(),
		Resource: (&otelv1.InboundLogRecord_Resource_builder{
			Attributes: []*otelv1.InboundLogRecord_KeyValue{
				logStringAttribute("service.name", "pathological-size-test"),
			},
		}).Build(),
		ResourceSchemaUrl: &resourceSchemaURL,
		Scope: (&otelv1.InboundLogRecord_InstrumentationScope_builder{
			Name: new(originalScopeName),
			Attributes: []*otelv1.InboundLogRecord_KeyValue{
				logStringAttribute("scope.attribute", "scope-value"),
			},
		}).Build(),
		ScopeSchemaUrl: &scopeSchemaURL,
		Provenance: (&otelv1.InboundLogRecord_Provenance_builder{
			Source:         new("speakeasy"),
			OrganizationId: new(testLogOrganizationID),
			ProjectId:      new(testLogProjectID),
		}).Build(),
		Attributes: []*otelv1.InboundLogRecord_KeyValue{
			logStringAttribute("gen_ai.input.messages", `[{"role":"user","parts":[{"type":"text","content":"hello"}]}]`),
			logStringAttribute("gen_ai.output.messages", `[{"role":"assistant","parts":[{"type":"text","content":"done"}],"finish_reason":"stop"}]`),
		},
	}).Build()
	padInboundLogRecordToSize(t, inbound, maxOTLPLogRecordBytes)
	require.Equal(t, maxOTLPLogRecordBytes, proto.Size(inbound))
	require.NoError(t, ValidateInboundLogRecord(inbound))

	var published *otelv1.LogRecord
	publisher := gcp.NewMockPublisher[*otelv1.LogRecord]()
	publisher.On("Publish", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		record, ok := args.Get(1).(*otelv1.LogRecord)
		require.True(t, ok)
		published = record
	}).Return(gcp.NewSuccessPublishResult()).Once()
	handler := NewLogTransformHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), publisher, newTestDatabase(t), cache.NoopCache)

	err := handler.Handle(t.Context(), inbound, gcp.MessageMetadata{})

	require.NoError(t, err)
	publisher.AssertExpectations(t)
	require.NotNil(t, published)
	attributes := make(map[string]*otelv1.LogRecord_AnyValue, len(published.GetAttributes()))
	for _, item := range published.GetAttributes() {
		attributes[item.GetKey()] = item.GetValue()
	}
	require.Equal(t, originalScopeName, attributes[string(OriginalInstrumentationScopeNameKey)].GetStringValue())
	require.Equal(t, testLogOrganizationID, attributes[string(OrganizationIDKey)].GetStringValue())
	require.Equal(t, testLogProjectID, attributes[string(ProjectIDKey)].GetStringValue())
	require.Positive(t, attributes[string(TokensCountKey)].GetIntValue())
	require.NotEmpty(t, attributes[string(TokensCodecKey)].GetStringValue())

	request, err := newLogRelayExportRequest([]*otelv1.LogRecord{published}, true)
	require.NoError(t, err)
	require.LessOrEqual(t, proto.Size(request), maxLogRelayExportBytes)
}

func padInboundLogRecordToSize(t *testing.T, record *otelv1.InboundLogRecord, target int) {
	t.Helper()

	body := record.GetBody()
	require.NotNil(t, body)
	bodyBytes := len(body.GetBytesValue())
	for range 4 {
		delta := target - proto.Size(record)
		if delta == 0 {
			return
		}
		bodyBytes += delta
		require.GreaterOrEqual(t, bodyBytes, 0)
		body.SetBytesValue(make([]byte, bodyBytes))
	}
	require.Equal(t, target, proto.Size(record))
}

func logStringAttribute(key, value string) *otelv1.InboundLogRecord_KeyValue {
	return (&otelv1.InboundLogRecord_KeyValue_builder{
		Key:   &key,
		Value: (&otelv1.InboundLogRecord_AnyValue_builder{StringValue: &value}).Build(),
	}).Build()
}
