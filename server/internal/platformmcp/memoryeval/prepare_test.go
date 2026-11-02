package memoryeval_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval/bench"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// echoModel answers every completion with the text of its last message, so a
// summary carries its masked transcript and a digest carries its inputs. It
// reports a fixed cost per call so spend accounting can be checked.
type echoModel struct {
	calls atomic.Int64
}

func (m *echoModel) GetCompletion(_ context.Context, request openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	m.calls.Add(1)
	encoded, err := json.Marshal(request.Messages[len(request.Messages)-1])
	if err != nil {
		return nil, fmt.Errorf("encode message: %w", err)
	}
	var last struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(encoded, &last); err != nil {
		return nil, fmt.Errorf("decode message: %w", err)
	}
	text := strings.TrimSuffix(strings.TrimPrefix(last.Content, "<transcript>\n"), "\n</transcript>")
	cost := 0.001
	message := openrouter.CreateMessageAssistant(text)
	return &openrouter.CompletionResponse{Message: &message, Content: text, Usage: openrouter.Usage{PromptTokens: 100, CompletionTokens: 10, TotalTokens: 110, Cost: &cost}}, nil
}

func (*echoModel) GetCompletionStream(context.Context, openrouter.CompletionRequest) (openrouter.StreamReader, error) {
	return nil, errors.New("not scripted")
}

func (*echoModel) GetObjectCompletion(context.Context, openrouter.ObjectCompletionRequest) (*openrouter.CompletionResponse, error) {
	return nil, errors.New("not scripted")
}

func (*echoModel) CreateEmbeddings(context.Context, string, string, []string, ...openrouter.EmbeddingOption) ([][]float32, error) {
	return nil, errors.New("not scripted")
}

func (*echoModel) ResolveKey(context.Context, string, string, billing.ModelUsageSource, openrouter.KeyType) (openrouter.ResolvedKey, error) {
	return openrouter.ResolvedKey{Key: "scripted", Customer: false}, nil
}

// TestPrepareWritesSummariesAndDigests checks that Prepare writes a summary
// for every team chat with the chosen model, folds them into digests, and
// that search_team_memory then finds them.
func TestPrepareWritesSummariesAndDigests(t *testing.T) {
	t.Parallel()
	corpus := memoryeval.Build(corpusNow)
	loaded := loadCorpus(t, "memoryeval_prepare", corpus)
	model := &echoModel{}
	models := bench.MemoryModels{Summary: "anthropic/claude-sonnet-5.5", Digest: "openai/gpt-5.5"}

	prep, err := bench.Prepare(t.Context(), testenv.NewLogger(t), model, loaded.target(), models, 8)
	require.NoError(t, err)

	team := 0
	for _, chat := range corpus.Chats {
		if !chat.Private {
			team++
		}
	}
	require.Equal(t, team, prep.Summaries, "every team chat gets a summary and private sessions none")
	require.Zero(t, prep.Failed)
	require.Positive(t, prep.Digests)
	t.Logf("%d summaries, %d digests", prep.Summaries, prep.Digests)
	require.Equal(t, models, prep.Models)
	require.Equal(t, team, prep.SummarySpend.Calls)
	require.InDelta(t, 0.001*float64(team), prep.SummarySpend.CostUSD, 1e-9)
	require.Equal(t, prep.Digests, prep.DigestSpend.Calls, "one digest call per digest written")
	require.Equal(t, int64(team+prep.Digests), model.calls.Load())

	// Rebuilding with another digest model replaces the digests rather than
	// adding to them.
	again, err := bench.Prepare(t.Context(), testenv.NewLogger(t), model, loaded.target(), bench.MemoryModels{Summary: models.Summary, Digest: "anthropic/claude-sonnet-5"}, 8)
	require.NoError(t, err)
	require.Equal(t, prep.Digests, again.Digests)

	memoryArm, err := bench.SearchTeamMemoryArm(testenv.NewLogger(t), loaded.target())
	require.NoError(t, err)
	q := questionByID(t, corpus, corpus.Questions[0].ID)
	require.NotEmpty(t, q.Evidence)
	result, err := memoryArm.Search(t.Context(), q.Evidence[0][0])
	require.NoError(t, err)
	require.NotEmpty(t, result.ChatIDs, "search_team_memory finds the prepared summaries")
}
