package provenance_test

import (
	"encoding/json"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/provenance"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestNativeLifecycle(t *testing.T) {
	t.Parallel()
	var meta any
	require.NoError(t, json.Unmarshal([]byte(`{"io.modelcontextprotocol.registry/official":{"status":"deprecated","isLatest":false,"updatedAt":"2026-08-01T00:00:00Z"},"com.speakeasy.ai/registry":{"publishedAt":"2026-07-01T00:00:00Z"}}`), &meta))
	got := provenance.Read(meta)
	require.False(t, got.Catalogued, "only actual matching establishes membership")
	require.Equal(t, "deprecated", got.Status)
	require.NotNil(t, got.IsLatest)
	require.False(t, *got.IsLatest)
	require.False(t, got.PublishedAt.IsZero())
	_, known := got.StaleFor(time.Now())
	require.True(t, known)
	got.Catalogued = true
	require.True(t, got.Withdrawn())
}

func TestAbsentEvidenceStaysUnknown(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{`null`, `{}`, `{"com.speakeasy.ai/catalog":{"supportsDcr":true}}`, `{"com.pulsemcp/server":{"isOfficial":true,"visitorsEstimateTotal":42},"com.pulsemcp/server-version":{"status":"deleted","updatedAt":"2026-08-01T00:00:00Z"}}`} {
		var meta any
		require.NoError(t, json.Unmarshal([]byte(raw), &meta))
		got := provenance.Read(meta)
		require.False(t, got.Catalogued)
		require.Nil(t, got.IsLatest)
		require.Empty(t, got.Status)
		require.True(t, got.PublishedAt.IsZero())
		_, known := got.StaleFor(time.Now())
		require.False(t, known)
		require.False(t, got.Withdrawn())
	}
}
