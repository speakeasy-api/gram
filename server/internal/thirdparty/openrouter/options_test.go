package openrouter

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/partition"
	"github.com/speakeasy-api/gram/server/internal/staff"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestNewUsesPartitionEndpoints(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name           string
		partition      partition.Partition
		wantInference  string
		wantManagement string
	}{
		{name: "us", partition: partition.US, wantInference: OpenRouterBaseURL, wantManagement: OpenRouterBaseURL},
		{name: "eu", partition: partition.EU, wantInference: EUBaseURL, wantManagement: OpenRouterBaseURL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tracerProvider := testenv.NewTracerProvider(t)
			policy, err := guardian.NewUnsafePolicy(tracerProvider, nil)
			require.NoError(t, err)

			client := New(testenv.NewLogger(t), tracerProvider, policy, nil, "test", tc.partition, "provisioning-key", nil, nil, nil, staff.Organization{})
			require.Equal(t, tc.wantInference, client.inferenceURL)
			require.Equal(t, tc.wantManagement, client.managementURL)
			require.Equal(t, tc.partition, client.partition)
		})
	}
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

			client := New(testenv.NewLogger(t), tracerProvider, policy, nil, "test", partition.US, "provisioning-key", nil, nil, nil, staff.Organization{}, option)
			require.Equal(t, strings.TrimSuffix(testURL, "/"), client.inferenceURL)
			require.Equal(t, strings.TrimSuffix(testURL, "/"), client.managementURL)
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
