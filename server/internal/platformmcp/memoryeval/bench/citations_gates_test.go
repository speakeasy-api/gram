package bench

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

func questionByID(t *testing.T, corpus *memoryeval.Corpus, id string) memoryeval.Question {
	t.Helper()
	for _, q := range corpus.Questions {
		if q.ID == id {
			return q
		}
	}
	t.Fatalf("no question %s", id)
	return memoryeval.Question{}
}

// TestCitationCreditIsPartialOnlyForMultiChatQuestions checks that an answer
// citing valid sources plus an unrelated chat earns the valid share on a
// multi-chat question and nothing on a single-chat one.
func TestCitationCreditIsPartialOnlyForMultiChatQuestions(t *testing.T) {
	t.Parallel()
	corpus := memoryeval.Build(testNow)

	var multi memoryeval.Question
	var valid []uuid.UUID
	for _, q := range corpus.Questions {
		if len(q.Sources) < 2 {
			continue
		}
		valid = valid[:0]
		for _, key := range q.Sources {
			chat, _ := corpus.Chat(key)
			if corpus.ChatCitationValid(q, chat.ID) {
				valid = append(valid, chat.ID)
			}
		}
		if len(valid) >= 2 {
			multi = q
			break
		}
	}
	require.NotEmpty(t, multi.ID, "the corpus has a multi-chat question with two citable sources")
	single := questionByID(t, corpus, "q24")
	singleSource, _ := corpus.Chat(single.Sources[0])
	unrelated := uuid.New()

	chat := func(id uuid.UUID) citation { return citation{kind: "chat", id: id, resolved: false, chats: nil} }

	strict, credit := citationCredit(corpus, multi, []citation{chat(valid[0]), chat(valid[1]), chat(unrelated)})
	require.False(t, strict)
	require.InDelta(t, 2.0/3, credit, 1e-9)

	strict, credit = citationCredit(corpus, multi, []citation{chat(valid[0]), chat(valid[0]), chat(valid[1])})
	require.True(t, strict, "a repeated citation counts once")
	require.InDelta(t, 1.0, credit, 1e-9)

	strict, credit = citationCredit(corpus, single, []citation{chat(singleSource.ID), chat(unrelated)})
	require.False(t, strict)
	require.Zero(t, credit, "single-chat questions stay all or nothing")

	strict, credit = citationCredit(corpus, multi, []citation{{kind: "memo", id: uuid.New(), resolved: false, chats: nil}})
	require.False(t, strict)
	require.Zero(t, credit, "nothing checkable earns nothing")
}

func gateStatuses(result GateResult) map[string]GateStatus {
	statuses := map[string]GateStatus{}
	for _, g := range result.Gates {
		statuses[g.Name] = g.Status
	}
	return statuses
}

// TestEvaluateGatesComparesCandidatesWithTheBaseline checks every gate
// against arm B for the same model, and that a model without a baseline row
// leaves the relative gates unknown.
func TestEvaluateGatesComparesCandidatesWithTheBaseline(t *testing.T) {
	t.Parallel()
	hard := func(correct, total int) map[memoryeval.QuestionKind]KindScore {
		return map[memoryeval.QuestionKind]KindScore{memoryeval.QuestionSynthesis: {Correct: correct, Total: total}}
	}
	base := ArmScore{Model: "m/one", Arm: BaselineArm, HasTools: true, AccuracyByRun: []float64{0.83}, Accuracy: 0.83, ByKind: hard(10, 15), Answerable: 45, CitationCredit: 23, ToolTokensPerQuestion: 3691, Negatives: 5, NotFound: 5}
	good := ArmScore{Model: "m/one", Arm: "D: recall", HasTools: true, AccuracyByRun: []float64{0.9}, Accuracy: 0.9, ByKind: hard(13, 15), Answerable: 45, CitationCredit: 43, ToolTokensPerQuestion: 5000, Negatives: 5, NotFound: 4, Leaks: nil, IsolationViolations: nil}
	bad := ArmScore{Model: "m/one", Arm: "C: search_team_memory", HasTools: true, AccuracyByRun: []float64{0.73}, Accuracy: 0.73, ByKind: hard(8, 15), Answerable: 45, CitationCredit: 23, ToolTokensPerQuestion: 8000, Negatives: 5, NotFound: 3, IsolationViolations: []string{"canary"}, Ungraded: 1}
	orphan := ArmScore{Model: "m/two", Arm: "C: search_team_memory", HasTools: true, AccuracyByRun: []float64{0.85}, Accuracy: 0.85, ByKind: hard(9, 15), Answerable: 45, CitationCredit: 45, ToolTokensPerQuestion: 100}
	report := &Report{Scores: []ArmScore{{Model: "m/one", Arm: "A: no memory"}, base, good, bad, orphan}}

	results := EvaluateGates(report)
	require.Len(t, results, 3, "only tool arms other than the baseline are candidates")

	require.Equal(t, "D: recall", results[0].Arm)
	require.True(t, results[0].Passed, "%+v", results[0].Gates)

	require.False(t, results[1].Passed)
	require.Equal(t, map[string]GateStatus{
		"Complete run": GateFail, "Hard-set lift": GateFail, "Overall accuracy": GateFail, "Privacy": GateFail,
		"Citations": GateFail, "Negatives": GateFail, "Tool tokens": GateFail,
	}, gateStatuses(results[1]))

	require.False(t, results[2].Passed)
	require.Equal(t, map[string]GateStatus{
		"Complete run": GatePass, "Hard-set lift": GateUnknown, "Overall accuracy": GateUnknown, "Privacy": GatePass,
		"Citations": GatePass, "Negatives": GateUnknown, "Tool tokens": GateUnknown,
	}, gateStatuses(results[2]))

	markdown := gatesMarkdown(results)
	require.Contains(t, markdown, "### D: recall, m/one: PASS (7 of 7 gates)")
	require.Contains(t, markdown, "### C: search_team_memory, m/one: FAIL (0 of 7 gates)")
	require.Contains(t, markdown, "| Hard-set lift | at least B + 15 points | 87% vs B 67% | PASS |")
}

type fixedCostClient struct {
	openrouter.CompletionClient
	cost  float64
	calls int
}

func (c *fixedCostClient) GetCompletion(context.Context, openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	c.calls++
	var response openrouter.CompletionResponse
	cost := c.cost
	response.Usage.Cost = &cost
	return &response, nil
}

// TestSpendCapStopsTheRun checks that reaching the cap cancels the context
// with the cap as its cause and refuses later calls without spending.
func TestSpendCapStopsTheRun(t *testing.T) {
	t.Parallel()
	inner := &fixedCostClient{cost: 4}
	ctx, client, stop := WithSpendCap(t.Context(), inner, 10)
	defer stop()

	for range 3 {
		_, err := client.GetCompletion(ctx, openrouter.CompletionRequest{})
		require.NoError(t, err)
	}
	require.Error(t, ctx.Err())
	capErr, ok := errors.AsType[*SpendCapError](context.Cause(ctx))
	require.True(t, ok)
	require.InDelta(t, 12.0, capErr.SpentUSD, 1e-9)

	_, err := client.GetCompletion(ctx, openrouter.CompletionRequest{})
	require.ErrorIs(t, err, capErr)
	require.Equal(t, 3, inner.calls, "a call after the cap never reaches the model")

	uncappedCtx, uncapped, stopUncapped := WithSpendCap(t.Context(), inner, 0)
	defer stopUncapped()
	require.Same(t, inner, uncapped)
	require.NoError(t, uncappedCtx.Err())
}

func TestCostCapScalesWithModelsAndRuns(t *testing.T) {
	t.Parallel()
	cfg := Config{AnswerModels: []string{"a", "b"}, JudgeModel: "j", Runs: 3, Concurrency: 1}
	require.InDelta(t, 6*CostCapPerPassUSD, cfg.CostCapUSD(), 1e-9)
}
