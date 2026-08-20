package otel

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
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
			OrganizationId: new("organization-id"),
			ProjectId:      new("project-id"),
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
	handler := NewLogTransformHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), publisher)

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
	require.Equal(t, "organization-id", attributes[string(OrganizationIDKey)].GetStringValue())
	require.Equal(t, "project-id", attributes[string(ProjectIDKey)].GetStringValue())
	require.Positive(t, attributes[string(TokensCountKey)].GetIntValue())
	require.NotEmpty(t, attributes[string(TokensCodecKey)].GetStringValue())
	require.Contains(t, attributes, "gen_ai.input.messages")
}

func logStringAttribute(key, value string) *otelv1.InboundLogRecord_KeyValue {
	return (&otelv1.InboundLogRecord_KeyValue_builder{
		Key:   &key,
		Value: (&otelv1.InboundLogRecord_AnyValue_builder{StringValue: &value}).Build(),
	}).Build()
}
