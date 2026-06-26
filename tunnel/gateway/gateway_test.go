package gateway

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/tunnel/wire"
)

func TestParseServiceMetadata(t *testing.T) {
	t.Parallel()

	metadata, err := parseServiceMetadata(`{"environment":"prod","blank":"","empty_key":"ok"," ":"ignored"}`)
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"environment": "prod",
		"empty_key":   "ok",
	}, metadata)
}

func TestParseServiceMetadataRejectsOversizedMetadata(t *testing.T) {
	t.Parallel()

	_, err := parseServiceMetadata(`{"value":"` + strings.Repeat("a", wire.MaxServiceMetadataBytes) + `"}`)
	require.ErrorIs(t, err, errServiceMetadataTooLarge)
}
