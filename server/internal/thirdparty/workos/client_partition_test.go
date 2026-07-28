package workos

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPartitionByHostAndAPIKey(t *testing.T) {
	t.Parallel()

	const apiKey = "sk_test_do-not-expose"
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://API.WORKOS.COM/users", nil)
	require.NoError(t, err)

	partition := partitionByHostAndAPIKey(apiKey)(req)
	require.Len(t, partition, 3)
	require.Equal(t, []string{"api.workos.com", "443"}, partition[:2])
	require.NotContains(t, strings.Join(partition, ":"), apiKey)
	require.Equal(t, partition, partitionByHostAndAPIKey(apiKey)(req))
	require.NotEqual(t, partition, partitionByHostAndAPIKey("sk_test_other")(req))
}
