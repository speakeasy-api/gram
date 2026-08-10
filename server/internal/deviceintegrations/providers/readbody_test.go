package providers_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/providers"
)

func TestReadBoundedBodyDetectsTruncation(t *testing.T) {
	t.Parallel()

	body, err := providers.ReadBoundedBody(strings.NewReader("small"), 16)
	require.NoError(t, err)
	require.Equal(t, "small", string(body))

	body, err = providers.ReadBoundedBody(strings.NewReader("exactly-16-chars"), 16)
	require.NoError(t, err, "a body exactly at the limit is not truncated")
	require.Equal(t, "exactly-16-chars", string(body))

	_, err = providers.ReadBoundedBody(strings.NewReader(strings.Repeat("x", 17)), 16)
	require.ErrorContains(t, err, "limit", "an oversized body must fail loudly, not decode a truncated payload")
}
