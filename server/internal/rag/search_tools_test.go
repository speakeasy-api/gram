package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
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

	schemaSummary, topLevelSchemaSummary := summarizeInputSchemaLevels(entry.InputSchema)
	content := buildEmbeddableContent(entry, []string{"source:http", "records"}, schemaSummary)
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
	require.Equal(t, `create_record
Create a record.
parameters:
- alpha
- zeta: Last field.`, buildTopLevelEmbeddableContent(entry, topLevelSchemaSummary))
	require.Equal(t, `create_record
Create a record.`, buildNameDescriptionEmbeddableContent(entry))
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

func TestSelectEmbeddingCandidateContents_UsesSelectedSizeForBatching(t *testing.T) {
	t.Parallel()

	denseContent := strings.Repeat("{}", 9_000)
	candidates := []embeddingCandidate{
		{content: denseContent, fallbacks: []string{"compact-one"}},
		{content: denseContent, fallbacks: []string{"compact-two"}},
		{content: denseContent, fallbacks: []string{"compact-three"}},
	}
	batchLimit := len(denseContent) * 2
	require.Len(t, createBatchesWithinSize(candidates, batchLimit), 2)

	selections, err := selectEmbeddingCandidateContents(defaultEmbeddingModel, candidates)
	require.NoError(t, err)
	require.Equal(t, []embeddingFallbackSelection{
		{candidateIndex: 0, strategy: embeddingFallbackStrategyTopLevelSchema},
		{candidateIndex: 1, strategy: embeddingFallbackStrategyTopLevelSchema},
		{candidateIndex: 2, strategy: embeddingFallbackStrategyTopLevelSchema},
	}, selections)
	require.Equal(t, "compact-one", candidates[0].content)
	require.Equal(t, "compact-two", candidates[1].content)
	require.Equal(t, "compact-three", candidates[2].content)
	require.Len(t, createBatchesWithinSize(candidates, batchLimit), 1)
}

func TestEmitEmbeddingFallbackLogs_IncludesCustomerAndToolContext(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	candidates := []embeddingCandidate{{
		toolID:   "tool-id",
		entryKey: "urn:gram:tool:tool-id",
	}}
	logContext := embeddingFallbackLogContext{
		organizationID:   "org-id",
		organizationSlug: "org-slug",
		projectID:        "project-id",
		projectSlug:      "project-slug",
		toolsetID:        "toolset-id",
		toolsetSlug:      "toolset-slug",
		model:            defaultEmbeddingModel,
	}

	emitEmbeddingFallbackLogs(
		context.Background(),
		logger,
		logContext,
		candidates,
		[]embeddingFallbackSelection{{
			candidateIndex: 0,
			strategy:       embeddingFallbackStrategyNameDescription,
		}},
	)

	var record map[string]any
	require.NoError(t, json.Unmarshal(output.Bytes(), &record))
	require.Equal(t, "WARN", record["level"])
	require.Equal(t, "tool embedding input exceeded token limit; using fallback", record["msg"])
	require.Equal(t, "org-id", record[string(attr.OrganizationIDKey)])
	require.Equal(t, "org-slug", record[string(attr.OrganizationSlugKey)])
	require.Equal(t, "project-id", record[string(attr.ProjectIDKey)])
	require.Equal(t, "project-slug", record[string(attr.ProjectSlugKey)])
	require.Equal(t, "toolset-id", record[string(attr.ToolsetIDKey)])
	require.Equal(t, "toolset-slug", record[string(attr.ToolsetSlugKey)])
	require.Equal(t, "tool-id", record[string(attr.ToolIDKey)])
	require.Equal(t, "urn:gram:tool:tool-id", record[string(attr.ToolURNKey)])
	require.Equal(t, defaultEmbeddingModel, record[string(attr.GenAIRequestModelKey)])
	require.Equal(t, embeddingFallbackStrategyNameDescription, record[string(attr.EmbeddingFallbackStrategyKey)])
}
