package openrouter

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestNewUsesProductionBaseURLByDefault(t *testing.T) {
	t.Parallel()

	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, nil)
	require.NoError(t, err)

	client := New(testenv.NewLogger(t), tracerProvider, policy, nil, "test", "provisioning-key", nil, nil, nil, nil)
	require.Equal(t, OpenRouterBaseURL, client.baseURL)
}

func TestWithTestBaseURLAcceptsAndNormalizesLoopbackHTTP(t *testing.T) {
	t.Parallel()

	for _, testURL := range []string{
		"http://localhost:8080",
		"http://127.0.0.1:8080/",
		"http://[::1]:8080",
	} {
		t.Run(testURL, func(t *testing.T) {
			t.Parallel()

			option, err := WithTestBaseURL(testURL)
			require.NoError(t, err)

			tracerProvider := testenv.NewTracerProvider(t)
			policy, err := guardian.NewUnsafePolicy(tracerProvider, nil)
			require.NoError(t, err)

			client := New(testenv.NewLogger(t), tracerProvider, policy, nil, "test", "provisioning-key", nil, nil, nil, nil, option)
			require.Equal(t, strings.TrimSuffix(testURL, "/"), client.baseURL)
		})
	}
}

func TestWithTestBaseURLRejectsUnsafeURLs(t *testing.T) {
	t.Parallel()

	for _, testURL := range []string{
		"https://openrouter.ai/api",
		"https://127.0.0.1:8080",
		"://malformed",
		"/relative",
		"http://user:password@localhost:8080",
		"http://localhost:8080/api",
		"http://localhost:8080?query=value",
		"http://localhost:8080#fragment",
	} {
		t.Run(testURL, func(t *testing.T) {
			t.Parallel()

			option, err := WithTestBaseURL(testURL)
			require.Error(t, err)
			require.Nil(t, option)
		})
	}
}
