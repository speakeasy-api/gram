package githubapp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClientCredential(t *testing.T) {
	t.Parallel()
	expires := time.Now().Add(time.Hour)
	forgotten := false
	credential := newClientCredential("test-secret", expires, func(context.Context) (bool, error) {
		forgotten = true
		return true, nil
	})
	require.Equal(t, "test-secret", credential.Value())
	require.Equal(t, "Bearer", credential.Scheme())
	require.Equal(t, expires, credential.ExpiresAt())
	for _, format := range []string{"%v", "%+v", "%#v"} {
		require.NotContains(t, fmt.Sprintf(format, credential), "test-secret")
	}
	encoded, err := json.Marshal(credential)
	require.NoError(t, err)
	require.JSONEq(t, "{}", string(encoded))
	require.NotContains(t, credential.LogValue().String(), "test-secret")
	renewed, err := credential.Forget(t.Context())
	require.NoError(t, err)
	require.True(t, renewed)
	require.True(t, forgotten)

	var empty ClientCredential
	require.Empty(t, empty.Value())
	renewed, err = empty.Forget(t.Context())
	require.NoError(t, err)
	require.False(t, renewed)
}
