package o11y_test

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestSetupOTelSDK(t *testing.T) {
	logger := testenv.NewLogger(t)
	shutdown, err := o11y.SetupOTelSDK(t.Context(), logger, o11y.SetupOTelSDKOptions{
		ServiceName:    "gram-test",
		ServiceVersion: "0.1.0",
		EnableTracing:  true,
		EnableMetrics:  true,
	})
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	err = shutdown(t.Context())
	require.NoError(t, err)
}
