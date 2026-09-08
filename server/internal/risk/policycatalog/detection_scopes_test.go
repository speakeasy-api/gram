package policycatalog

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDetectionScopeCodecRoundTripsCanonicalMessageTypes(t *testing.T) {
	t.Parallel()

	catalog, err := Build()
	require.NoError(t, err)

	encoded, err := EncodeDetectionScope([]string{"user_message", "tool_response", "user_message"}, catalog)
	require.NoError(t, err)
	require.Equal(t, `kind in ["tool_response","user_message"]`, encoded)

	decoded, ok := DecodeDetectionScope(encoded, "", catalog)
	require.True(t, ok)
	require.Equal(t, []string{"tool_response", "user_message"}, decoded)
}

func TestDetectionScopeCodecRejectsNonCanonicalAndLegacyCEL(t *testing.T) {
	t.Parallel()

	catalog, err := Build()
	require.NoError(t, err)

	for _, value := range []string{
		`kind == "user_message"`,
		`kind in ["user_message","tool_response"]`,
		`kind in ["user_message","user_message"]`,
		`kind in ["prompt_attachment"]`,
	} {
		_, ok := DecodeDetectionScope(value, "", catalog)
		require.False(t, ok, value)
	}
	_, ok := DecodeDetectionScope(`kind in ["user_message"]`, `kind == "assistant_message"`, catalog)
	require.False(t, ok)
}
