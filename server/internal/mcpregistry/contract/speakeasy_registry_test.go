package contract

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSpeakeasyRegistryMetadataOverlay(t *testing.T) {
	t.Parallel()
	s, err := Compile(Schema)
	require.NoError(t, err)
	value, err := decode([]byte(`{"server":{"name":"example.test/demo","description":"Demo","version":"1"},"_meta":{"com.speakeasy.ai/catalog":{"documentationUrl":"ftp://example.test/docs"}}}`))
	require.NoError(t, err)
	require.Error(t, s.Validate(value))
}

func TestOverlayPreservesRootAllOf(t *testing.T) {
	t.Parallel()
	s, err := Compile([]byte(`{"allOf":[{"required":["extension"]}]}`))
	require.NoError(t, err)
	require.Error(t, s.Validate(map[string]any{}))
	require.NoError(t, s.Validate(map[string]any{"extension": true}))
	for _, raw := range []string{`{"allOf":false}`, `{"allOf":[]}`} {
		_, err := Compile([]byte(raw))
		require.Error(t, err, raw)
	}
}
