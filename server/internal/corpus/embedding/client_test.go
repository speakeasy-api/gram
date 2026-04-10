package embedding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// openAIEmbeddingResponse mirrors the OpenAI embeddings API response shape.
type openAIEmbeddingResponse struct {
	Object string                       `json:"object"`
	Data   []openAIEmbeddingResponseObj `json:"data"`
	Model  string                       `json:"model"`
	Usage  openAIUsage                  `json:"usage"`
}

type openAIEmbeddingResponseObj struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

type openAIUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

func newMockOpenAIServer(t *testing.T, dims int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1/embeddings", r.URL.Path)
		require.Contains(t, r.Header.Get("Authorization"), "Bearer ")

		var reqBody struct {
			Input []string `json:"input"`
			Model string   `json:"model"`
		}
		err := json.NewDecoder(r.Body).Decode(&reqBody)
		require.NoError(t, err)

		var data []openAIEmbeddingResponseObj
		for i, _ := range reqBody.Input {
			vec := make([]float32, dims)
			for j := range vec {
				vec[j] = float32(i+1) * 0.001
			}
			data = append(data, openAIEmbeddingResponseObj{
				Object:    "embedding",
				Index:     i,
				Embedding: vec,
			})
		}

		resp := openAIEmbeddingResponse{
			Object: "list",
			Data:   data,
			Model:  reqBody.Model,
			Usage:  openAIUsage{PromptTokens: len(reqBody.Input) * 10, TotalTokens: len(reqBody.Input) * 10},
		}

		w.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(w).Encode(resp)
		require.NoError(t, err)
	}))
}

func TestBatchEmbed(t *testing.T) {
	server := newMockOpenAIServer(t, EmbeddingDimensions)
	defer server.Close()

	client := NewClient("test-key", server.URL)

	texts := []string{"one", "two", "three", "four", "five"}
	requests := make([]BatchEmbedRequest, len(texts))
	for i, text := range texts {
		requests[i] = BatchEmbedRequest{
			ChunkID:     text,
			Text:        text,
			Fingerprint: Fingerprint(text, "h2", "{}", ""),
		}
	}

	results, err := BatchEmbed(context.Background(), client, requests, nil)
	require.NoError(t, err)
	require.Len(t, results, 5)

	for _, res := range results {
		require.False(t, res.Skipped)
		require.Len(t, res.Embedding, EmbeddingDimensions)
	}
}

func TestBatchEmbed_SkipUnchanged(t *testing.T) {
	server := newMockOpenAIServer(t, EmbeddingDimensions)
	defer server.Close()

	client := NewClient("test-key", server.URL)

	fp1 := Fingerprint("hello", "h2", "{}", "")
	fp2 := Fingerprint("world", "h2", "{}", "")

	requests := []BatchEmbedRequest{
		{ChunkID: "chunk-1", Text: "hello", Fingerprint: fp1},
		{ChunkID: "chunk-2", Text: "world", Fingerprint: fp2},
	}

	// Pretend chunk-1 already has the same fingerprint stored.
	existing := map[string]string{
		"chunk-1": fp1,
	}

	results, err := BatchEmbed(context.Background(), client, requests, existing)
	require.NoError(t, err)
	require.Len(t, results, 2)

	// chunk-1 should be skipped; chunk-2 should be embedded.
	var skipped, embedded int
	for _, res := range results {
		if res.Skipped {
			skipped++
			require.Equal(t, "chunk-1", res.ChunkID)
		} else {
			embedded++
			require.Equal(t, "chunk-2", res.ChunkID)
			require.Len(t, res.Embedding, EmbeddingDimensions)
		}
	}
	require.Equal(t, 1, skipped)
	require.Equal(t, 1, embedded)
}
