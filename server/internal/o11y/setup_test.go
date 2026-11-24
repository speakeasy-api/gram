package o11y_test

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/partition"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestSetupOTelSDK(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	shutdown, err := o11y.SetupOTelSDK(t.Context(), logger, o11y.SetupOTelSDKOptions{
		ServiceName:    "gram-test",
		ServiceVersion: "0.1.0",
		Partition:      partition.US,
		EnableTracing:  true,
		EnableMetrics:  true,
	})
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	err = shutdown(t.Context())
	require.NoError(t, err)
}

func TestSetupOTelSDK_RejectsMissingPartition(t *testing.T) {
	t.Parallel()

	shutdown, err := o11y.SetupOTelSDK(t.Context(), testenv.NewLogger(t), o11y.SetupOTelSDKOptions{
		ServiceName:    "gram-test",
		ServiceVersion: "0.1.0",
		EnableTracing:  false,
		EnableMetrics:  false,
	})
	require.ErrorContains(t, err, `otel resource partition: unknown partition ""`)
	require.Nil(t, shutdown)
}
