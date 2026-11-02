package memoryeval_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/stretchr/testify/require"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/guardian"
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
	ctx, searchArm := searchChatsArm(t, loaded)
	noMemory := arm{name: "A: no memory", search: nil, sweep: nil, tools: nil, call: nil, instructions: ""}
	arms := []arm{noMemory, searchArm}
	model := scriptedModel{searchFrom: corpusNow.AddDate(0, 0, -30).Format(time.RFC3339), searchTo: corpusNow.Add(24 * time.Hour).Format(time.RFC3339)}

	answers, verdicts := runEval(ctx, t, model, "scripted-answer", "scripted-judge", corpus, arms, 1, 4)
	stats := summarize(corpus, 1, arms, answers, verdicts)
	report := buildReport(corpus, "scripted-answer", "scripted-judge", 1, arms, answers, verdicts)
	t.Log("\n" + report)
	require.Contains(t, report, "| B: search_chats | 2% (2–2%)")

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

// TestSummarizeLeavesErrorsOutOfAccuracy checks that an answer that failed
// for API reasons counts as an error, not as a wrong answer.
func TestSummarizeLeavesErrorsOutOfAccuracy(t *testing.T) {
	t.Parallel()
	corpus := memoryeval.Build(corpusNow)
	a := arm{name: "A: no memory", search: nil, sweep: nil, tools: nil, call: nil, instructions: ""}
	answers := []answer{
		{arm: a.name, run: 0, question: corpus.Questions[0], text: "right", citations: nil, toolOutput: "", err: nil},
		{arm: a.name, run: 0, question: corpus.Questions[1], text: "", citations: nil, toolOutput: "", err: errors.New("rate limited")},
	}
	verdicts := []verdict{
		{Correct: true, Miss: "", Reason: "ok"},
		judge(t.Context(), scriptedModel{searchFrom: "", searchTo: ""}, "scripted-judge", answers[1]),
	}

	s := summarize(corpus, 1, []arm{a}, answers, verdicts)[a.name]
	require.InDelta(t, 1.0, s.accuracyByRun[0], 1e-9)
	require.Equal(t, 1, s.errors)
}

func TestParseVerdictRejectsIncompleteOrContradictoryReplies(t *testing.T) {
	t.Parallel()
	ok, err := parseVerdict(`{"correct": false, "miss": "reasoning", "reason": "wrong library"}`)
	require.NoError(t, err)
	require.Equal(t, verdict{Correct: false, Miss: "reasoning", Reason: "wrong library"}, ok)

	for _, reply := range []string{
		`{"miss": "", "reason": "no verdict"}`,
		`{"correct": true, "miss": "retrieval", "reason": "both"}`,
		`{"correct": false, "miss": "", "reason": "no miss"}`,
		`not json`,
	} {
		_, err := parseVerdict(reply)
		require.Error(t, err, reply)
	}
}

// keyResolverSentinel stops a completion right after request validation.
type keyResolverSentinel struct{}

var errReachedKeyResolver = errors.New("reached key resolution")

func (keyResolverSentinel) ResolveKey(context.Context, string, string, billing.ModelUsageSource, openrouter.KeyType) (openrouter.ResolvedKey, error) {
	return openrouter.ResolvedKey{}, errReachedKeyResolver
}

// TestCompletionRequestPassesClientValidation runs the eval's request through
// the real completion client. Key resolution happens after validation, so
// reaching it proves the client would send the request to the model.
func TestCompletionRequestPassesClientValidation(t *testing.T) {
	t.Parallel()
	client := openrouter.NewUnifiedClient(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		guardian.NewDefaultPolicy(tracenoop.NewTracerProvider()),
		openrouter.NewDevelopment("unused"),
		keyResolverSentinel{},
		nil, nil, nil, nil,
	)
	_, err := client.GetCompletion(t.Context(), completionRequest("scripted-answer", []or.ChatMessages{openrouter.CreateMessageUser("hi")}, nil, nil))
	require.ErrorIs(t, err, errReachedKeyResolver)
}

func TestRequireSomeSucceededPassesWithOneSuccess(t *testing.T) {
	t.Parallel()
	errs := []error{errors.New("rate limited"), nil}
	requireSomeSucceeded(t, "answer", len(errs), func(i int) error { return errs[i] })
}
