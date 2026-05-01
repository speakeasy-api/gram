// Package externalmcptest provides helpers for constructing
// [externalmcp.RegistryClient] instances in tests.
package externalmcptest

import (
	"log/slog"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// NewRegistryClient builds an [externalmcp.RegistryClient] wired to a
// pulsemcp.com URL with placeholder credentials, suitable for tests that
// don't actually exercise the HTTP layer.
func NewRegistryClient(t *testing.T, logger *slog.Logger, tracerProvider trace.TracerProvider) *externalmcp.RegistryClient {
	t.Helper()

	pulseURL, err := url.Parse("https://api.pulsemcp.com")
	require.NoError(t, err, "expected pulse URL to parse")

	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err, "expected guardian policy to initialize without error")

	return externalmcp.NewRegistryClient(
		testenv.NewLogger(t),
		tracerProvider,
		guardianPolicy,
		externalmcp.NewPulseBackend(pulseURL, "test-tenant-id", conv.NewSecret([]byte("test-api-key"))),
		nil,
	)
}
