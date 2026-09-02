package openrouter

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	"github.com/tiktoken-go/tokenizer"
)

func TestLimitEmbeddingInputs_OpenAIUsesTokenBoundary(t *testing.T) {
	t.Parallel()

	codec, err := tokenizer.Get(tokenizer.Cl100kBase)
	require.NoError(t, err)

	dense := strings.Repeat("{}", maxOpenAIEmbeddingInputTokens+500)
	denseTokens, err := codec.Count(dense)
	require.NoError(t, err)
	require.Greater(t, denseTokens, maxOpenAIEmbeddingInputTokens)
	require.Less(t, len(dense), legacyMaxEmbeddingInputBytes, "fixture must bypass the old byte limit")

	underLimit := strings.Repeat("hello ", 1_500)
	underLimitTokens, err := codec.Count(underLimit)
	require.NoError(t, err)
	require.LessOrEqual(t, underLimitTokens, maxOpenAIEmbeddingInputTokens)
	require.Greater(t, len(underLimit), maxOpenAIEmbeddingInputTokens, "fixture must exercise tokenization")

	limited, truncatedCount, err := limitEmbeddingInputs(openAITextEmbedding3Small, []string{"short", dense, underLimit})
	require.NoError(t, err)
	require.Equal(t, 1, truncatedCount)
	require.Equal(t, "short", limited[0])
	require.Equal(t, underLimit, limited[2])
	require.NotEqual(t, dense, limited[1])

	limitedTokens, err := codec.Count(limited[1])
	require.NoError(t, err)
	require.LessOrEqual(t, limitedTokens, maxOpenAIEmbeddingInputTokens)
}

func TestLimitEmbeddingInputs_OpenAIPreservesUTF8(t *testing.T) {
	t.Parallel()

	input := strings.Repeat("😀", maxOpenAIEmbeddingInputTokens)
	limited, truncatedCount, err := limitEmbeddingInputs(openAITextEmbedding3Small, []string{input})
	require.NoError(t, err)
	require.Equal(t, 1, truncatedCount)
	require.True(t, utf8.ValidString(limited[0]))

	codec, err := tokenizer.Get(tokenizer.Cl100kBase)
	require.NoError(t, err)
	limitedTokens, err := codec.Count(limited[0])
	require.NoError(t, err)
	require.LessOrEqual(t, limitedTokens, maxOpenAIEmbeddingInputTokens)
}

func TestLimitEmbeddingInputs_OtherModelsPreserveLegacyByteLimit(t *testing.T) {
	t.Parallel()

	input := strings.Repeat("x", legacyMaxEmbeddingInputBytes+1)
	limited, truncatedCount, err := limitEmbeddingInputs("qwen/qwen3-embedding-8b", []string{input})
	require.NoError(t, err)
	require.Equal(t, 1, truncatedCount)
	require.Len(t, limited[0], legacyMaxEmbeddingInputBytes)
}
