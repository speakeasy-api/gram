package contract

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSpeakeasyRegistryMetadataOverlay(t *testing.T) {
	t.Parallel()
	s, err := Compile(Schema)
	require.NoError(t, err)
	value, err := decode([]byte(`{"server":{"name":"example.test/demo","description":"Demo","version":"1"},"_meta":{"com.speakeasy.ai/catalog":{"documentationUrl":"ftp://example.test/docs"}}}`))
	require.NoError(t, err)
	require.Error(t, s.Validate(value))
}
