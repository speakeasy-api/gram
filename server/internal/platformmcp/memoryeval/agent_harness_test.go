package memoryeval_test

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

var (
	scriptedProjectID = regexp.MustCompile(`project_id is ([0-9a-f-]{36})`)
	scriptedAnswerKey = regexp.MustCompile(`(?m)^Answer key: (.*)$`)
	scriptedCandidate = regexp.MustCompile(`(?ms)^Assistant's answer: (.*?)\n\nGrade`)
	scriptedKeyWord   = regexp.MustCompile(`[A-Za-z]{4,}`)
)

// scriptedModel stands in for both the answering model and the judge, so the
// harness can be checked without LLM calls. With tools it searches once for
// PgCat and answers with what it found; without tools it answers Not found.
// As judge it accepts Not found for a Not found key, and otherwise an answer
// that contains the key's first word of four or more letters.
type scriptedModel struct {
	searchFrom, searchTo string
}

func (m scriptedModel) GetCompletion(_ context.Context, request openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	encoded, err := json.Marshal(request.Messages)
	if err != nil {
		return nil, err
	}
	transcript := string(encoded)
	if strings.Contains(transcript, "You grade answers") {
		var prompt []struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(encoded, &prompt); err != nil {
			return nil, err
		}
		key := scriptedAnswerKey.FindStringSubmatch(prompt[0].Content)[1]
		candidate := scriptedCandidate.FindStringSubmatch(prompt[0].Content)[1]
		correct := candidate == "Not found"
		if key != memoryeval.NotFound {
			correct = !correct && strings.Contains(candidate, scriptedKeyWord.FindString(key))
		}
		return &openrouter.CompletionResponse{Content: `{"correct": ` + strconv.FormatBool(correct) + `, "miss": "", "reason": "scripted"}`}, nil
	}
	if len(request.Tools) == 0 {
		return &openrouter.CompletionResponse{Content: "Not found"}, nil
	}
	if !strings.Contains(transcript, `"role":"tool"`) {
		project := scriptedProjectID.FindStringSubmatch(transcript)[1]
		args, err := json.Marshal(map[string]string{"project_id": project, "from": m.searchFrom, "to": m.searchTo, "query": "PgCat"})
		if err != nil {
			return nil, err
		}
		return &openrouter.CompletionResponse{ToolCalls: []openrouter.ToolCall{{ID: "call-1", Type: "function", Function: openrouter.ToolCallFunction{Name: "search_chats", Arguments: string(args)}}}}, nil
	}
	return &openrouter.CompletionResponse{Content: "PgCat replaced PgBouncer [chat:" + memoryeval.ChatID("reversal-pooler").String() + "]"}, nil
}

func (scriptedModel) GetCompletionStream(context.Context, openrouter.CompletionRequest) (openrouter.StreamReader, error) {
	return nil, errors.New("not scripted")
}

func (scriptedModel) GetObjectCompletion(context.Context, openrouter.ObjectCompletionRequest) (*openrouter.CompletionResponse, error) {
	return nil, errors.New("not scripted")
}

func (scriptedModel) CreateEmbeddings(context.Context, string, string, []string, ...openrouter.EmbeddingOption) ([][]float32, error) {
	return nil, errors.New("not scripted")
}

func (scriptedModel) ResolveKey(context.Context, string, string, billing.ModelUsageSource, openrouter.KeyType) (openrouter.ResolvedKey, error) {
	return openrouter.ResolvedKey{Key: "scripted", Customer: false}, nil
}

// TestJudgedEvalHarnessScoresScriptedAnswers checks the agent loop, tool
// wiring, citation parsing and scoring end to end with a scripted model.
func TestJudgedEvalHarnessScoresScriptedAnswers(t *testing.T) {
	t.Parallel()
	corpus := memoryeval.Build(corpusNow)
	loaded := loadCorpus(t, "memoryeval_harness", corpus)
	ctx, searchArm := searchChatsArm(t, loaded)
	noMemory := arm{name: "A: no memory", search: nil, sweep: nil, tools: nil, call: nil, instructions: ""}
	arms := []arm{noMemory, searchArm}
	model := scriptedModel{searchFrom: corpusNow.AddDate(0, 0, -30).Format(time.RFC3339), searchTo: corpusNow.Add(24 * time.Hour).Format(time.RFC3339)}

	answers, verdicts := runEval(ctx, t, model, "scripted-answer", "scripted-judge", corpus, arms, 1, 4)
	stats := summarize(corpus, 1, arms, answers, verdicts)
	t.Log("\n" + renderReport(corpus, "scripted-answer", "scripted-judge", 1, arms, stats))

	none, search := stats[noMemory.name], stats[searchArm.name]
	require.InDelta(t, 10.0/40, none.accuracyByRun[0], 1e-9, "no memory answers Not found, which is right for the 10 negative and isolation questions")
	require.InDelta(t, 1.0/40, search.accuracyByRun[0], 1e-9, "the scripted search arm only answers the pooler question")
	require.Equal(t, 30, search.answerable)
	require.Equal(t, 30, search.citedAnswers)
	require.Equal(t, 1, search.validCited, "the reversal chat supports only the pooler question")
	require.Empty(t, search.leaks)
	require.Empty(t, search.isolation)
	require.Positive(t, search.toolTokens)
	require.Zero(t, none.toolTokens)
}
