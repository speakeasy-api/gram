package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	searchDocsToolName = "search_docs"
	getDocToolName     = "get_doc"
)

// DocsSearcher is the interface used by docs MCP tools to search and retrieve
// corpus content. Implementations wrap the corpus search service.
type DocsSearcher interface {
	SearchDocs(ctx context.Context, projectID string, query string, limit int) ([]DocsSearchResult, error)
	GetChunk(ctx context.Context, projectID string, chunkID string) (*DocsChunk, error)
}

// DocsSearchResult represents a single search result from the corpus.
type DocsSearchResult struct {
	ChunkID     string  `json:"chunk_id"`
	FilePath    string  `json:"file_path"`
	HeadingPath string  `json:"heading_path"`
	Breadcrumb  string  `json:"breadcrumb"`
	Content     string  `json:"content"`
	Score       float64 `json:"score"`
}

// DocsChunk is a chunk with its neighbor information.
type DocsChunk struct {
	ChunkID     string             `json:"chunk_id"`
	FilePath    string             `json:"file_path"`
	HeadingPath string             `json:"heading_path"`
	Breadcrumb  string             `json:"breadcrumb"`
	Content     string             `json:"content"`
	ContentText string             `json:"content_text"`
	Prev        *DocsNeighborChunk `json:"prev,omitempty"`
	Next        *DocsNeighborChunk `json:"next,omitempty"`
}

// DocsNeighborChunk is a minimal reference to an adjacent chunk.
type DocsNeighborChunk struct {
	ChunkID     string `json:"chunk_id"`
	HeadingPath string `json:"heading_path"`
	Breadcrumb  string `json:"breadcrumb"`
}

var searchDocsSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"query": {
			"type": "string",
			"description": "Natural language search query for documentation content."
		},
		"limit": {
			"type": "integer",
			"minimum": 1,
			"maximum": 20,
			"description": "Maximum number of results to return. Defaults to 10."
		}
	},
	"required": ["query"],
	"additionalProperties": false
}`)

var getDocSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"chunk_id": {
			"type": "string",
			"description": "The chunk ID to retrieve, as returned by search_docs."
		}
	},
	"required": ["chunk_id"],
	"additionalProperties": false
}`)

func buildDocsToolListEntries() []*toolListEntry {
	return []*toolListEntry{
		{
			Name:        searchDocsToolName,
			Description: "Search project documentation using a natural language query. Returns matching documentation chunks ranked by relevance.",
			InputSchema: searchDocsSchema,
			Annotations: nil,
			Meta:        nil,
		},
		{
			Name:        getDocToolName,
			Description: "Retrieve a specific documentation chunk by its ID, including neighboring chunks for context.",
			InputSchema: getDocSchema,
			Annotations: nil,
			Meta:        nil,
		},
	}
}

// marshalTextToolResult wraps a text payload in the MCP tool call response envelope.
func marshalTextToolResult(reqID msgID, text string) (json.RawMessage, error) {
	chunk, err := json.Marshal(contentChunk[string, json.RawMessage]{
		Type:     "text",
		Text:     text,
		MimeType: nil,
		Data:     nil,
		Meta:     nil,
	})
	if err != nil {
		return nil, err
	}

	return json.Marshal(result[toolCallResult]{
		ID: reqID,
		Result: toolCallResult{
			Content: []json.RawMessage{chunk},
			IsError: false,
		},
	})
}

type searchDocsArguments struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

func handleSearchDocsCall(
	ctx context.Context,
	logger *slog.Logger,
	reqID msgID,
	argsRaw json.RawMessage,
	projectID string,
	searcher DocsSearcher,
) (json.RawMessage, error) {
	var args searchDocsArguments
	if len(argsRaw) > 0 {
		if err := json.Unmarshal(argsRaw, &args); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "failed to parse search_docs arguments").Log(ctx, logger)
		}
	}

	query := strings.TrimSpace(args.Query)
	if query == "" {
		return nil, oops.E(oops.CodeInvalid, errors.New("missing query"), "query is required").Log(ctx, logger)
	}

	limit := args.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 20 {
		limit = 20
	}

	results, err := searcher.SearchDocs(ctx, projectID, query, limit)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to search documentation").Log(ctx, logger)
	}

	// Ensure nil slice serializes as empty array.
	if results == nil {
		results = []DocsSearchResult{}
	}

	type searchDocsResponse struct {
		Results []DocsSearchResult `json:"results"`
	}

	payload, err := json.Marshal(searchDocsResponse{Results: results})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize search_docs result").Log(ctx, logger)
	}

	response, err := marshalTextToolResult(reqID, string(payload))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize search_docs response").Log(ctx, logger)
	}

	return response, nil
}

type getDocArguments struct {
	ChunkID string `json:"chunk_id"`
}

func handleGetDocCall(
	ctx context.Context,
	logger *slog.Logger,
	reqID msgID,
	argsRaw json.RawMessage,
	projectID string,
	searcher DocsSearcher,
) (json.RawMessage, error) {
	var args getDocArguments
	if len(argsRaw) > 0 {
		if err := json.Unmarshal(argsRaw, &args); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "failed to parse get_doc arguments").Log(ctx, logger)
		}
	}

	chunkID := strings.TrimSpace(args.ChunkID)
	if chunkID == "" {
		return nil, oops.E(oops.CodeInvalid, errors.New("missing chunk_id"), "chunk_id is required").Log(ctx, logger)
	}

	chunk, err := searcher.GetChunk(ctx, projectID, chunkID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, oops.E(oops.CodeNotFound, err, "chunk not found").Log(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to retrieve documentation chunk").Log(ctx, logger)
	}

	payload, err := json.Marshal(chunk)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize get_doc result").Log(ctx, logger)
	}

	response, err := marshalTextToolResult(reqID, string(payload))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize get_doc response").Log(ctx, logger)
	}

	return response, nil
}
