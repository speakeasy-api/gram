package externalmcptest

import (
	"log/slog"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
)

func NewMCPRegistryClient(t *testing.T, logger *slog.Logger, tracerProvider trace.TracerProvider) *externalmcp.RegistryClient {
	t.Helper()

	pulseURL, err := url.Parse("https://api.pulsemcp.com")
	require.NoError(t, err, "expected pulse URL to parse")

	client := externalmcp.NewRegistryClient(
		logger,
		tracerProvider,
		externalmcp.NewPulseBackend(pulseURL, "test-tenant-id", conv.NewSecret([]byte("test-api-key"))),
		nil,
	)
	require.NoError(t, err, "expected mcp registry client to initialize without error")

	return client
}
