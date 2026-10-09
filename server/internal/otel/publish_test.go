package otel

import (
	"errors"
	"strings"
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func publishTestLogRecord(id string) *otelv1.InboundLogRecord {
	return (&otelv1.InboundLogRecord_builder{RecordId: &id}).Build()
}

func TestPublishRefusesTheWholeBatchWhenOneRecordIsInvalid(t *testing.T) {
	t.Parallel()

	publisher := gcp.NewMockPublisher[*otelv1.InboundLogRecord]()

	err := PublishLogs(t.Context(), publisher, []*otelv1.InboundLogRecord{publishTestLogRecord("a"), publishTestLogRecord("")})

	require.ErrorIs(t, err, ErrInvalid)
	publisher.AssertNotCalled(t, "Publish", mock.Anything, mock.Anything)
}

func TestPublishReturnsAPublishFailure(t *testing.T) {
	t.Parallel()

	publisher := gcp.NewMockPublisher[*otelv1.InboundLogRecord]()
	publisher.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewErrPublishResult(errors.New("pubsub unavailable"))).Once()

	err := PublishLogs(t.Context(), publisher, []*otelv1.InboundLogRecord{publishTestLogRecord("a")})

	require.ErrorContains(t, err, "pubsub unavailable")
	require.NotErrorIs(t, err, ErrInvalid, "a publish failure is not the producer's fault")
}

func TestValidateInboundLogRecordEnforcesTheIngestContract(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateInboundLogRecord(publishTestLogRecord("a")))
	require.ErrorContains(t, ValidateInboundLogRecord(nil), "required")
	require.ErrorContains(t, ValidateInboundLogRecord(publishTestLogRecord("")), "ID is required")

	oversized := publishTestLogRecord("a")
	oversized.SetSeverityText(strings.Repeat("x", maxOTLPLogRecordBytes))
	require.ErrorContains(t, ValidateInboundLogRecord(oversized), "exceeds maximum size")

	badTrace := publishTestLogRecord("a")
	badTrace.SetTraceId(make([]byte, 3))
	require.ErrorContains(t, ValidateInboundLogRecord(badTrace), "trace ID")

	badSpan := publishTestLogRecord("a")
	badSpan.SetSpanId(make([]byte, 3))
	require.ErrorContains(t, ValidateInboundLogRecord(badSpan), "span ID")
}
