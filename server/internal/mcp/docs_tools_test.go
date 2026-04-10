package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSearchDocsTool(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testLogger()
	reqID := msgID{format: 1, Number: 1}

	searcher := &stubDocsSearcher{
		searchResults: []DocsSearchResult{
			{
				ChunkID:     "chunk-001",
				FilePath:    "docs/getting-started.md",
				HeadingPath: "Getting Started > Installation",
				Breadcrumb:  "Getting Started / Installation",
				Content:     "# Installation\n\nRun `npm install gram`.",
				Score:       0.95,
			},
			{
				ChunkID:     "chunk-002",
				FilePath:    "docs/api-reference.md",
				HeadingPath: "API Reference > Authentication",
				Breadcrumb:  "API Reference / Authentication",
				Content:     "## Authentication\n\nUse API keys to authenticate.",
				Score:       0.82,
			},
		},
	}

	argsRaw := json.RawMessage(`{"query": "how to install"}`)
	projectID := "11111111-1111-1111-1111-111111111111"

	response, err := handleSearchDocsCall(ctx, logger, reqID, argsRaw, projectID, searcher)
	require.NoError(t, err)
	require.NotNil(t, response)

	// Verify the response contains the search results
	require.Contains(t, string(response), "chunk-001")
	require.Contains(t, string(response), "docs/getting-started.md")
	require.Contains(t, string(response), "Installation")
	require.Contains(t, string(response), "chunk-002")
	require.Contains(t, string(response), "docs/api-reference.md")

	// Verify the searcher received the correct query
	require.Equal(t, "how to install", searcher.lastQuery)
	require.Equal(t, projectID, searcher.lastProjectID)
}

func TestGetDocTool(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testLogger()
	reqID := msgID{format: 1, Number: 1}

	searcher := &stubDocsSearcher{
		getChunkResult: &DocsChunk{
			ChunkID:     "chunk-001",
			FilePath:    "docs/getting-started.md",
			HeadingPath: "Getting Started > Installation",
			Breadcrumb:  "Getting Started / Installation",
			Content:     "# Installation\n\nRun `npm install gram`.",
			ContentText: "Installation Run npm install gram.",
			Prev: &DocsNeighborChunk{
				ChunkID:     "chunk-000",
				HeadingPath: "Getting Started > Overview",
				Breadcrumb:  "Getting Started / Overview",
			},
			Next: &DocsNeighborChunk{
				ChunkID:     "chunk-002",
				HeadingPath: "Getting Started > Configuration",
				Breadcrumb:  "Getting Started / Configuration",
			},
		},
	}

	argsRaw := json.RawMessage(`{"chunk_id": "chunk-001"}`)
	projectID := "11111111-1111-1111-1111-111111111111"

	response, err := handleGetDocCall(ctx, logger, reqID, argsRaw, projectID, searcher)
	require.NoError(t, err)
	require.NotNil(t, response)

	// Verify the response contains the chunk content
	require.Contains(t, string(response), "chunk-001")
	require.Contains(t, string(response), "docs/getting-started.md")
	require.Contains(t, string(response), "Installation")
	require.Contains(t, string(response), "npm install gram")

	// Verify neighbor info is included
	require.Contains(t, string(response), "chunk-000")
	require.Contains(t, string(response), "chunk-002")

	// Verify the searcher received the correct parameters
	require.Equal(t, "chunk-001", searcher.lastChunkID)
	require.Equal(t, projectID, searcher.lastProjectID)
}

func TestSearchDocsTool_EmptyCorpus(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testLogger()
	reqID := msgID{format: 1, Number: 1}

	searcher := &stubDocsSearcher{
		searchResults: nil, // no results
	}

	argsRaw := json.RawMessage(`{"query": "anything"}`)
	projectID := "11111111-1111-1111-1111-111111111111"

	response, err := handleSearchDocsCall(ctx, logger, reqID, argsRaw, projectID, searcher)
	require.NoError(t, err)
	require.NotNil(t, response)

	// Should return a valid response with empty results, not an error
	var parsed result[toolCallResult]
	err = json.Unmarshal(response, &parsed)
	require.NoError(t, err)
	require.False(t, parsed.Result.IsError)
	require.Len(t, parsed.Result.Content, 1)

	// The text content should indicate empty results
	require.Contains(t, string(parsed.Result.Content[0]), "results")
}

// stubDocsSearcher is a test stub implementing DocsSearcher.
type stubDocsSearcher struct {
	searchResults  []DocsSearchResult
	getChunkResult *DocsChunk
	searchErr      error
	getChunkErr    error

	lastQuery     string
	lastProjectID string
	lastChunkID   string
	lastLimit     int
}

func (s *stubDocsSearcher) SearchDocs(_ context.Context, projectID string, query string, limit int) ([]DocsSearchResult, error) {
	s.lastProjectID = projectID
	s.lastQuery = query
	s.lastLimit = limit
	return s.searchResults, s.searchErr
}

func (s *stubDocsSearcher) GetChunk(_ context.Context, projectID string, chunkID string) (*DocsChunk, error) {
	s.lastProjectID = projectID
	s.lastChunkID = chunkID
	return s.getChunkResult, s.getChunkErr
}
