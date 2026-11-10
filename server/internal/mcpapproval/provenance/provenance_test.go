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
	require.Equal(t, "deprecated", got.Status)
	require.Equal(t, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), got.PublishedAt)
	require.Equal(t, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), got.UpdatedAt)
}

func TestRegistryPublicationTimeWinsOverPublisherMetadata(t *testing.T) {
	t.Parallel()
	var meta any
	require.NoError(t, json.Unmarshal([]byte(`{"io.modelcontextprotocol.registry/official":{"publishedAt":"2020-01-01T00:00:00Z"},"com.speakeasy.ai/registry":{"publishedAt":"2026-07-01T00:00:00Z"}}`), &meta))
	require.Equal(t, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), provenance.Read(meta).PublishedAt)
}

func TestOfficialPublicationTimeFillsInWhenRegistryRecordsNone(t *testing.T) {
	t.Parallel()
	var meta any
	require.NoError(t, json.Unmarshal([]byte(`{"io.modelcontextprotocol.registry/official":{"publishedAt":"2026-06-01T00:00:00Z"}}`), &meta))
	require.Equal(t, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), provenance.Read(meta).PublishedAt)
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

func TestMalformedNamespacesPreserveOtherEvidence(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		meta string
		want provenance.Provenance
	}{
		{
			name: "malformed registry publication falls back to official",
			meta: `{"com.speakeasy.ai/registry":{"publishedAt":42},"io.modelcontextprotocol.registry/official":{"status":"active","publishedAt":"2026-06-01T00:00:00Z","updatedAt":"2026-08-01T00:00:00Z"}}`,
			want: provenance.Provenance{
				Status:      "active",
				PublishedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				UpdatedAt:   time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "malformed official lifecycle preserves registry publication",
			meta: `{"io.modelcontextprotocol.registry/official":{"status":42,"updatedAt":false},"com.speakeasy.ai/registry":{"publishedAt":"2026-07-01T00:00:00Z"}}`,
			want: provenance.Provenance{
				PublishedAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var meta any
			require.NoError(t, json.Unmarshal([]byte(tt.meta), &meta))
			require.Equal(t, tt.want, provenance.Read(meta))
		})
	}
}
