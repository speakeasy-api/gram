package gramotel

import (
	"context"
	"errors"
	"strings"
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// orderedResult fails the test if it is awaited before every publish of the
// batch has been issued.
type orderedResult struct {
	t         *testing.T
	published *int
	want      int
}

func (r *orderedResult) Ready() <-chan struct{} {
	ready := make(chan struct{})
	close(ready)
	return ready
}

func (r *orderedResult) Get(context.Context) (string, error) {
	r.t.Helper()
	require.Equal(r.t, r.want, *r.published, "every publish must be issued before any result is awaited")
	return "message-id", nil
}

func testLogRecord(id string) *otelv1.InboundLogRecord {
	return (&otelv1.InboundLogRecord_builder{RecordId: &id}).Build()
}

func TestPublishIssuesEveryPublishBeforeAwaitingAnyResult(t *testing.T) {
	t.Parallel()

	published := 0
	result := &orderedResult{t: t, published: &published, want: 3}
	publisher := gcp.NewMockPublisher[*otelv1.InboundLogRecord]()
	publisher.On("Publish", mock.Anything, mock.Anything).Run(func(mock.Arguments) { published++ }).Return(result).Times(3)

	err := PublishLogs(t.Context(), nil, publisher, []*otelv1.InboundLogRecord{testLogRecord("a"), testLogRecord("b"), testLogRecord("c")})

	require.NoError(t, err)
	publisher.AssertExpectations(t)
}

func TestPublishRefusesTheWholeBatchWhenOneRecordIsInvalid(t *testing.T) {
	t.Parallel()

	publisher := gcp.NewMockPublisher[*otelv1.InboundLogRecord]()

	err := PublishLogs(t.Context(), nil, publisher, []*otelv1.InboundLogRecord{testLogRecord("a"), testLogRecord("")})

	require.ErrorIs(t, err, ErrInvalid)
	publisher.AssertNotCalled(t, "Publish", mock.Anything, mock.Anything)
}

func TestPublishReturnsAPublishFailure(t *testing.T) {
	t.Parallel()

	publisher := gcp.NewMockPublisher[*otelv1.InboundLogRecord]()
	publisher.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewErrPublishResult(errors.New("pubsub unavailable"))).Once()

	err := PublishLogs(t.Context(), nil, publisher, []*otelv1.InboundLogRecord{testLogRecord("a")})

	require.ErrorContains(t, err, "pubsub unavailable")
	require.NotErrorIs(t, err, ErrInvalid, "a publish failure is not the producer's fault")
}

func TestValidateLogRecordEnforcesTheIngestContract(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateLogRecord(testLogRecord("a")))
	require.ErrorContains(t, ValidateLogRecord(nil), "required")
	require.ErrorContains(t, ValidateLogRecord(testLogRecord("")), "ID is required")

	oversized := testLogRecord("a")
	oversized.SetSeverityText(strings.Repeat("x", MaxLogRecordBytes))
	require.ErrorContains(t, ValidateLogRecord(oversized), "exceeds maximum size")

	badTrace := testLogRecord("a")
	badTrace.SetTraceId(make([]byte, 3))
	require.ErrorContains(t, ValidateLogRecord(badTrace), "trace ID")

	badSpan := testLogRecord("a")
	badSpan.SetSpanId(make([]byte, 3))
	require.ErrorContains(t, ValidateLogRecord(badSpan), "span ID")
}
