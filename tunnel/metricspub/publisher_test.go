package metricspub

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	tunnelv1 "github.com/speakeasy-api/gram/infra/gen/gram/tunnel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
)

func TestShutdownFlushesAfterRuntimeCancellationBeforeStoppingPublisher(t *testing.T) {
	t.Parallel()
	pub := gcp.NewMockPublisher[*tunnelv1.MetricsSnapshot]()
	published := pub.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewSuccessPublishResult()).Twice()
	pub.On("Stop", mock.Anything).Return(nil).Once().NotBefore(published)
	ctx, cancel := context.WithCancel(t.Context())
	collector, stop := Start(ctx, pub)
	collector.Observe("source", "server", "tools/list", "claude", "attempt", 0)
	cancel()
	shutdown, done := context.WithTimeout(t.Context(), time.Second)
	defer done()
	require.NoError(t, stop(shutdown))
	pub.AssertExpectations(t)
}
