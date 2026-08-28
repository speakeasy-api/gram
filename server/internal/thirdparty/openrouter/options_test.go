package openrouter

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestNewBaseURLDefaultsAndCanBeInjected(t *testing.T) {
	t.Parallel()

	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, nil)
	require.NoError(t, err)

	defaultClient := New(testenv.NewLogger(t), tracerProvider, policy, nil, "test", "provisioning-key", nil, nil, nil, nil)
	require.Equal(t, OpenRouterBaseURL, defaultClient.baseURL)

	injectedClient := New(testenv.NewLogger(t), tracerProvider, policy, nil, "test", "provisioning-key", nil, nil, nil, nil, WithBaseURL("https://openrouter.invalid"))
	require.Equal(t, "https://openrouter.invalid", injectedClient.baseURL)
}
