package memoryeval_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval/bench"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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
// PgCat and cites the pooler reversal only if the search returned it; without
// tools it answers Not found.
// As judge it accepts Not found for a Not found key, and otherwise an answer
// that contains the key's first word of four or more letters.
type scriptedModel struct {
	searchFrom, searchTo string
}

func (m scriptedModel) GetCompletion(_ context.Context, request openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	encoded, err := json.Marshal(request.Messages)
	if err != nil {
		return nil, fmt.Errorf("encode messages: %w", err)
	}
	transcript := string(encoded)
	if strings.Contains(transcript, "You grade answers") {
		var prompt []struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(encoded, &prompt); err != nil {
			return nil, fmt.Errorf("decode judge prompt: %w", err)
		}
		keyMatch := scriptedAnswerKey.FindStringSubmatch(prompt[0].Content)
		candidateMatch := scriptedCandidate.FindStringSubmatch(prompt[0].Content)
		if keyMatch == nil || candidateMatch == nil {
			return nil, errors.New("scripted judge: answer key or candidate not found in prompt")
		}
		key, candidate := keyMatch[1], candidateMatch[1]
		correct := candidate == "Not found"
		if key != memoryeval.NotFound {
			correct = !correct && strings.Contains(candidate, scriptedKeyWord.FindString(key))
		}
		return &openrouter.CompletionResponse{Content: `{"correct": ` + strconv.FormatBool(correct) + `, "miss": "` + scriptedMiss(correct) + `", "reason": "scripted"}`}, nil
	}
	if len(request.Tools) == 0 {
		return &openrouter.CompletionResponse{Content: "Not found"}, nil
	}
	if !strings.Contains(transcript, `"role":"tool"`) {
		match := scriptedProjectID.FindStringSubmatch(transcript)
		if match == nil {
			return nil, errors.New("scripted model: project_id not found in prompt")
		}
		args, err := json.Marshal(map[string]string{"project_id": match[1], "from": m.searchFrom, "to": m.searchTo, "query": "PgCat"})
		if err != nil {
			return nil, fmt.Errorf("encode search arguments: %w", err)
		}
		return &openrouter.CompletionResponse{ToolCalls: []openrouter.ToolCall{{ID: "call-1", Type: "function", Function: openrouter.ToolCallFunction{Name: "search_chats", Arguments: string(args)}}}}, nil
	}
	// Answer only from what the tool returned, so broken tool wiring shows up
	// as a Not found instead of a scripted success.
	pooler := memoryeval.ChatID("reversal-pooler").String()
	if !strings.Contains(transcript, `\"chat_id\":\"`+pooler+`\"`) {
		return &openrouter.CompletionResponse{Content: "Not found"}, nil
	}
	return &openrouter.CompletionResponse{Content: "PgCat replaced PgBouncer [chat:" + pooler + "]"}, nil
}

func scriptedMiss(correct bool) string {
	if correct {
		return ""
	}
	return "retrieval"
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
	searchArm, err := bench.SearchChatsArm(testenv.NewLogger(t), loaded.target())
	require.NoError(t, err)
	noMemory := bench.NoMemoryArm()
	model := scriptedModel{searchFrom: corpusNow.AddDate(0, 0, -30).Format(time.RFC3339), searchTo: corpusNow.Add(24 * time.Hour).Format(time.RFC3339)}

	// The scripted model ignores the model name, so two names check that a
	// run scores every answer model separately.
	cfg := bench.Config{AnswerModels: []string{"anthropic/claude-sonnet-5.5", "google/gemini-3.1-flash-lite"}, JudgeModel: "openai/gpt-5.5", Runs: 1, Concurrency: 4}
	report, err := bench.Run(t.Context(), model, corpus, []bench.Arm{noMemory, searchArm}, cfg)
	require.NoError(t, err)
	markdown := report.Markdown()
	t.Log("\n" + markdown)
	require.Contains(t, markdown, "| google/gemini-3.1-flash-lite | B: search_chats | 2% (2–2%)")
	require.Len(t, report.Scores, 4)

	for _, model := range cfg.AnswerModels {
		var none, search bench.ArmScore
		for _, s := range report.Scores {
			switch {
			case s.Model == model && s.Arm == noMemory.Name:
				none = s
			case s.Model == model && s.Arm == searchArm.Name:
				search = s
			}
		}
		require.InDelta(t, 10.0/40, none.AccuracyByRun[0], 1e-9, "no memory answers Not found, which is right for the 10 negative and isolation questions")
		require.InDelta(t, 1.0/40, search.AccuracyByRun[0], 1e-9, "the scripted search arm only answers the pooler question")
		require.Equal(t, 30, search.Answerable)
		require.Equal(t, 30, search.CitedAnswers)
		require.Equal(t, 1, search.ValidCited, "the reversal chat supports only the pooler question")
		require.Empty(t, search.Leaks)
		require.Empty(t, search.IsolationViolations)
		require.Positive(t, search.ToolTokensPerQuestion)
		require.Zero(t, none.ToolTokensPerQuestion)
		require.True(t, search.HasTools)
		require.False(t, none.HasTools)
		require.Equal(t, 40, search.Answers)
	}
}
