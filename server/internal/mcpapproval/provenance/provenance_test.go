package provenance_test

import (
	"encoding/json"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/provenance"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNativeLifecycle(t *testing.T) {
	t.Parallel()
	var meta any
	require.NoError(t, json.Unmarshal([]byte(`{"io.modelcontextprotocol.registry/official":{"status":"deprecated","isLatest":false,"updatedAt":"2026-08-01T00:00:00Z"},"com.speakeasy.ai/registry":{"publishedAt":"2026-07-01T00:00:00Z"}}`), &meta))
	got := provenance.Read(meta)
	require.Equal(t, "deprecated", got.Status)
	require.False(t, got.PublishedAt.IsZero())
	require.False(t, got.UpdatedAt.IsZero())
}

func TestAbsentEvidenceStaysUnknown(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{`null`, `{}`, `{"com.speakeasy.ai/catalog":{"supportsDcr":true}}`, `{"com.pulsemcp/server":{"isOfficial":true,"visitorsEstimateTotal":42},"com.pulsemcp/server-version":{"status":"deleted","updatedAt":"2026-08-01T00:00:00Z"}}`} {
		var meta any
		require.NoError(t, json.Unmarshal([]byte(raw), &meta))
		got := provenance.Read(meta)
		require.Empty(t, got.Status)
		require.True(t, got.PublishedAt.IsZero())
		require.True(t, got.UpdatedAt.IsZero())
	}
}

func TestLatestMetadataDoesNotAffectFacts(t *testing.T) {
	t.Parallel()
	baseline := provenance.Read(map[string]any{"io.modelcontextprotocol.registry/official": map[string]any{"status": "active"}})
	for _, latest := range []bool{false, true} {
		got := provenance.Read(map[string]any{"io.modelcontextprotocol.registry/official": map[string]any{"status": "active", "isLatest": latest}})
		require.Equal(t, baseline, got)
	}
}
