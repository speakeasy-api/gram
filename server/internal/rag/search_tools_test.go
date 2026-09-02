package rag

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildEmbeddableContent_SummarizesSchemaDeterministically(t *testing.T) {
	t.Parallel()

	entry := &toolListEntry{
		Name:        "create_record",
		Description: "Create a record.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"description":"Input values.",
			"required":["zeta"],
			"properties":{
				"zeta":{"type":"string","description":" Last field. "},
				"alpha":{
					"type":"array",
					"items":{
						"type":"object",
						"required":["id"],
						"properties":{
							"mode":{"type":"string","enum":["fast","safe"]},
							"id":{"type":"string","format":"uuid"}
						}
					}
				}
			}
		}`),
		Meta: map[string]any{
			"ui": "metadata is retained in the payload, not embedded",
		},
	}

	content := buildEmbeddableContent(entry, []string{"source:http", "records"})
	require.Equal(t, `create_record
Create a record.
tags: source:http, records
parameters:
- input (object): Input values.
- alpha (array)
- alpha[] (object)
- alpha[].id (required, string, format=uuid)
- alpha[].mode (string, enum=[fast, safe])
- zeta (required, string): Last field.`, content)
	require.NotContains(t, content, "metadata is retained")
}

func TestSummarizeInputSchema_MalformedSchemaFallsBackToOriginal(t *testing.T) {
	t.Parallel()

	require.Equal(t, "{malformed", summarizeInputSchema(json.RawMessage("{malformed")))
}

func TestSummarizeInputSchema_BoundsDescriptionsAndEnums(t *testing.T) {
	t.Parallel()

	longDescription := strings.Repeat("x", 321)
	schema, err := json.Marshal(map[string]any{
		"type":        "string",
		"description": longDescription,
		"enum":        []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"},
	})
	require.NoError(t, err)

	summary := summarizeInputSchema(schema)
	require.Contains(t, summary, "enum=[a, b, c, d, e, f, g, h, i, j, k, l] +1 more")
	require.Contains(t, summary, strings.Repeat("x", 320)+"…")
	require.NotContains(t, summary, longDescription)
}

func TestCreateBatchesBySize_RespectsAggregateInputLimit(t *testing.T) {
	t.Parallel()

	denseContent := strings.Repeat("{}", embeddingMaxBatchBytes/4)
	candidates := []embeddingCandidate{
		{content: denseContent},
		{content: denseContent},
		{content: denseContent},
	}

	batches := (&ToolsetVectorStore{}).createBatchesBySize(candidates)
	require.Equal(t, []embeddingBatch{
		{startIdx: 0, endIdx: 2},
		{startIdx: 2, endIdx: 3},
	}, batches)

	for _, batch := range batches {
		totalBytes := 0
		for _, candidate := range candidates[batch.startIdx:batch.endIdx] {
			totalBytes += len(candidate.content)
		}
		require.LessOrEqual(t, totalBytes, embeddingMaxBatchBytes)
	}
}
