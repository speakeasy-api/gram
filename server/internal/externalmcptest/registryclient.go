// Package externalmcptest provides helpers for constructing
// [externalmcp.RegistryClient] instances in tests.
package externalmcptest

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/guardian"
)

// NewRegistryClient builds a credential-free registry client for tests.
func NewRegistryClient(t *testing.T, logger *slog.Logger, tracerProvider trace.TracerProvider) *externalmcp.RegistryClient {
	t.Helper()

	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err, "expected guardian policy to initialize without error")

	return externalmcp.NewRegistryClient(
		logger,
		tracerProvider,
		guardianPolicy,
		nil,
		cache.NoopCache,
	)
}
