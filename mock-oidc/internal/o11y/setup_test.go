package o11y_test

import (
	"log/slog"
	"testing"

	"github.com/speakeasy-api/gram/mock-oidc/internal/o11y"
	"github.com/stretchr/testify/require"
)

// The resource merges with resource.Default(), so the semconv import must use
// the same schema version as the OTel SDK or setup fails at boot.
func TestSetupOTelSDK(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(t.Output(), nil))
	shutdown, err := o11y.SetupOTelSDK(t.Context(), logger, o11y.SetupOTelSDKOptions{
		ServiceName:    "mock-oidc-test",
		ServiceVersion: "0.1.0",
		EnableTracing:  true,
		EnableMetrics:  true,
	})
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	require.NoError(t, shutdown(t.Context()))
}
