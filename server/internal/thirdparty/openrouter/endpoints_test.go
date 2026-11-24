package openrouter

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/partition"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestEndpointsFor(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		partition partition.Partition
		want      Endpoints
	}{
		{name: "us", partition: partition.US, want: Endpoints{Inference: "https://openrouter.ai/api", Management: "https://openrouter.ai/api", ServesDecisionModels: true}},
		{name: "eu", partition: partition.EU, want: Endpoints{Inference: "https://eu.openrouter.ai/api", Management: "https://openrouter.ai/api", ServesDecisionModels: false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.want, EndpointsFor(tc.partition))
		})
	}
}

func TestEndpointsForCoversEveryPartition(t *testing.T) {
	t.Parallel()

	for _, p := range partition.All() {
		require.NotPanics(t, func() { EndpointsFor(p) }, "partition %q needs OpenRouter endpoints", p)
	}
}

func TestEndpointsForPanicsOnUnknownPartition(t *testing.T) {
	t.Parallel()

	require.Panics(t, func() { EndpointsFor(partition.Partition("mars")) })
}

func TestInferenceClientsUseInferenceEndpoint(t *testing.T) {
	t.Parallel()

	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, nil)
	require.NoError(t, err)

	endpoints := EndpointsFor(partition.EU)

	chatClient := NewUnifiedClient(testenv.NewLogger(t), policy, endpoints, nil, nil, nil, nil, nil, nil)
	require.Equal(t, EUBaseURL, chatClient.inferenceURL)

	resolver := NewContextWindowResolver(testenv.NewLogger(t), policy, cache.NoopCache, endpoints)
	require.Equal(t, EUBaseURL, resolver.baseURL)
}
