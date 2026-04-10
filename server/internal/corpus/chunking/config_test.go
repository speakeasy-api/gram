package chunking_test

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/corpus/chunking"
	"github.com/stretchr/testify/require"
)

func TestParseDocsMcpConfig(t *testing.T) {
	t.Parallel()

	data := []byte(`{
		"version": "1",
		"strategy": {
			"chunk_by": "h2",
			"max_chunk_size": 2000,
			"min_chunk_size": 100
		},
		"metadata": {
			"department": "engineering"
		}
	}`)

	config, err := chunking.ParseDocsMcpConfig(data)
	require.NoError(t, err)
	require.Equal(t, "1", config.Version)
	require.NotNil(t, config.Strategy)
	require.Equal(t, "h2", config.Strategy.ChunkBy)
	require.Equal(t, 2000, config.Strategy.MaxChunkSize)
	require.Equal(t, 100, config.Strategy.MinChunkSize)
	require.Equal(t, "engineering", config.Metadata["department"])
}

func TestNearestManifestResolution(t *testing.T) {
	t.Parallel()

	rootConfig := &chunking.DocsMcpConfig{
		Version:   "1",
		Strategy:  &chunking.Strategy{ChunkBy: "h2", MaxChunkSize: 2000, MinChunkSize: 0},
		Metadata:  nil,
		Overrides: nil,
	}
	subConfig := &chunking.DocsMcpConfig{
		Version:   "1",
		Strategy:  &chunking.Strategy{ChunkBy: "h3", MaxChunkSize: 1000, MinChunkSize: 0},
		Metadata:  nil,
		Overrides: nil,
	}

	configs := map[string]*chunking.DocsMcpConfig{
		"":         rootConfig,
		"docs/api": subConfig,
	}

	// File in root uses root config
	resolved := chunking.ResolveConfig("README.md", configs)
	require.NotNil(t, resolved)
	require.Equal(t, "h2", resolved.Strategy.ChunkBy)

	// File in subdirectory uses subdirectory config
	resolved = chunking.ResolveConfig("docs/api/endpoints.md", configs)
	require.NotNil(t, resolved)
	require.Equal(t, "h3", resolved.Strategy.ChunkBy)

	// File in docs/ (not docs/api/) uses root config
	resolved = chunking.ResolveConfig("docs/guide.md", configs)
	require.NotNil(t, resolved)
	require.Equal(t, "h2", resolved.Strategy.ChunkBy)
}

func TestOverridePatternMatching(t *testing.T) {
	t.Parallel()

	config := &chunking.DocsMcpConfig{
		Version:  "1",
		Strategy: &chunking.Strategy{ChunkBy: "h2", MaxChunkSize: 2000, MinChunkSize: 0},
		Metadata: nil,
		Overrides: []chunking.Override{
			{
				Pattern:  "*.changelog.md",
				Strategy: &chunking.Strategy{ChunkBy: "file", MaxChunkSize: 5000, MinChunkSize: 0},
				Metadata: map[string]string{"type": "changelog"},
			},
		},
	}

	// Non-matching file gets default strategy
	strategy := chunking.ResolveStrategy("docs/guide.md", config)
	require.Equal(t, "h2", strategy.ChunkBy)

	// Matching file gets override strategy
	strategy = chunking.ResolveStrategy("CHANGELOG.changelog.md", config)
	require.Equal(t, "file", strategy.ChunkBy)
}
