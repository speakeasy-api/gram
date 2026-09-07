package otel

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type allQueuedPublishResult struct {
	t         *testing.T
	published *int
	want      int
}

func (r *allQueuedPublishResult) Ready() <-chan struct{} {
	ready := make(chan struct{})
	close(ready)
	return ready
}

func (r *allQueuedPublishResult) Get(context.Context) (string, error) {
	r.t.Helper()
	require.Equal(r.t, r.want, *r.published, "all items must be enqueued before any publish result is settled")
	return "message-id", nil
}

func TestIngestOTLPExportHandlesGzipAndSettlesAfterEnqueue(t *testing.T) {
	t.Parallel()

	raw := []byte("encoded OTLP export")
	var compressed bytes.Buffer
	compressor := gzip.NewWriter(&compressed)
	_, err := compressor.Write(raw)
	require.NoError(t, err)
	require.NoError(t, compressor.Close())

	published := 0
	result := &allQueuedPublishResult{t: t, published: &published, want: 2}
	publisher := gcp.NewMockPublisher[*wrapperspb.StringValue]()
	publisher.On("Publish", mock.Anything, mock.Anything).Run(func(mock.Arguments) {
		published++
	}).Return(result).Twice()
	encoding := "gzip"
	decoded := false
	validated := 0
	ctx := contextvalues.SetAuthContext(t.Context(), testOTELAuthContext(uuid.MustParse(testLogProjectID)))

	err = ingestOTLPExport(ctx, testenv.NewLogger(t), otlpIngestSpec[*wrapperspb.StringValue]{
		signal:          "test",
		contentEncoding: &encoding,
		body:            io.NopCloser(bytes.NewReader(compressed.Bytes())),
		decode: func(encoded []byte, tenant otlpIngestTenant) ([]*wrapperspb.StringValue, error) {
			decoded = true
			require.Equal(t, raw, encoded)
			require.Equal(t, testLogOrganizationID, tenant.organizationID)
			require.Equal(t, testLogProjectID, tenant.projectID)
			return []*wrapperspb.StringValue{wrapperspb.String("first"), wrapperspb.String("second")}, nil
		},
		validate: func(*wrapperspb.StringValue) error {
			validated++
			return nil
		},
		publisher: publisher,
	})

	require.NoError(t, err)
	require.True(t, decoded)
	require.Equal(t, 2, validated)
	publisher.AssertExpectations(t)
}
