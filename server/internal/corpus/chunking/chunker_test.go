package chunking_test

import (
	"strings"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/corpus/chunking"
	"github.com/stretchr/testify/require"
)

const h2Doc = `## Setup

Setup instructions here.

## Configuration

Configuration details here.

## Deployment

Deployment steps here.
`

func TestChunkByH2(t *testing.T) {
	t.Parallel()

	chunks, err := chunking.ChunkMarkdown("guide.md", []byte(h2Doc), chunking.Strategy{
		ChunkBy:      "h2",
		MaxChunkSize: 10000,
		MinChunkSize: 0,
	})
	require.NoError(t, err)
	require.Len(t, chunks, 3)

	require.Contains(t, chunks[0].Content, "## Setup")
	require.Contains(t, chunks[0].Content, "Setup instructions")
	require.Contains(t, chunks[1].Content, "## Configuration")
	require.Contains(t, chunks[1].Content, "Configuration details")
	require.Contains(t, chunks[2].Content, "## Deployment")
	require.Contains(t, chunks[2].Content, "Deployment steps")
}

const h3Doc = `# Guide

## Setup

### Prerequisites

Install Go first.

### Installation

Run go install.

## Usage

Use it like this.
`

func TestChunkByH3(t *testing.T) {
	t.Parallel()

	chunks, err := chunking.ChunkMarkdown("guide.md", []byte(h3Doc), chunking.Strategy{
		ChunkBy:      "h3",
		MaxChunkSize: 10000,
		MinChunkSize: 0,
	})
	require.NoError(t, err)
	require.Len(t, chunks, 3)

	require.Contains(t, chunks[0].Content, "### Prerequisites")
	require.Contains(t, chunks[0].Content, "Install Go")
	require.Contains(t, chunks[1].Content, "### Installation")
	require.Contains(t, chunks[1].Content, "go install")
	require.Contains(t, chunks[2].Content, "## Usage")
}

func TestChunkByFile(t *testing.T) {
	t.Parallel()

	doc := "# Single Doc\n\nThis is the entire file content.\n\n## Section\n\nMore content.\n"
	chunks, err := chunking.ChunkMarkdown("single.md", []byte(doc), chunking.Strategy{
		ChunkBy:      "file",
		MaxChunkSize: 10000,
		MinChunkSize: 0,
	})
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	require.Contains(t, chunks[0].Content, "Single Doc")
	require.Contains(t, chunks[0].Content, "More content")
}

func TestOversizedChunk(t *testing.T) {
	t.Parallel()

	// Create a large section that exceeds max_chunk_size
	bigContent := "## Big Section\n\n" + strings.Repeat("This is a long paragraph with many words. ", 100) + "\n"
	chunks, err := chunking.ChunkMarkdown("big.md", []byte(bigContent), chunking.Strategy{
		ChunkBy:      "h2",
		MaxChunkSize: 200,
		MinChunkSize: 0,
	})
	require.NoError(t, err)
	require.Greater(t, len(chunks), 1, "oversized section should be split into multiple chunks")

	for _, c := range chunks {
		require.LessOrEqual(t, len(c.ContentText), 400, "each chunk should be roughly within max size")
	}
}

func TestUndersizedChunk(t *testing.T) {
	t.Parallel()

	doc := "## Section A\n\nContent A is substantial enough.\n\n## Tiny\n\nX\n\n## Section B\n\nContent B here.\n"
	chunks, err := chunking.ChunkMarkdown("runt.md", []byte(doc), chunking.Strategy{
		ChunkBy:      "h2",
		MaxChunkSize: 10000,
		MinChunkSize: 50,
	})
	require.NoError(t, err)
	// The tiny "X" section should be merged with the preceding chunk
	require.Less(t, len(chunks), 3, "runt section should be merged")
}

func TestBreadcrumbs(t *testing.T) {
	t.Parallel()

	bc := chunking.GenerateBreadcrumb([]string{"Guide", "Setup", "Prerequisites"})
	require.Equal(t, "Guide > Setup > Prerequisites", bc)

	bc2 := chunking.GenerateBreadcrumb([]string{"Single"})
	require.Equal(t, "Single", bc2)
}

func TestDeterministicChunkID(t *testing.T) {
	t.Parallel()

	id := chunking.GenerateChunkID("guides/retries.md", "backoff-strategy/exponential")
	require.Equal(t, "guides/retries.md#backoff-strategy/exponential", id)

	id2 := chunking.GenerateChunkID("readme.md", "")
	require.Equal(t, "readme.md", id2)
}

func TestPlainTextExtraction(t *testing.T) {
	t.Parallel()

	md := []byte("# Title\n\nSome **bold** and *italic* text.\n\n```go\nfunc main() {}\n```\n\n[link](http://example.com)\n")
	text, err := chunking.ExtractPlainText(md)
	require.NoError(t, err)

	require.Contains(t, text, "Title")
	require.Contains(t, text, "bold")
	require.Contains(t, text, "italic")
	require.Contains(t, text, "link")
	require.NotContains(t, text, "**")
	require.NotContains(t, text, "```")
	require.NotContains(t, text, "[link]")
	require.NotContains(t, text, "http://example.com")
}
