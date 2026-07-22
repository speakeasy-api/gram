package openrouter

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	gramopenrouter "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

func typedVerdict(kind, target string, operational bool) Verdict {
	return Verdict{DirectiveKind: kind, Target: target, Operational: operational, Rationale: "safe rationale"}
}

func TestAggregateRequiresStrictMajorityAndCountsFailuresSafe(t *testing.T) {
	t.Parallel()

	attack := typedVerdict(DirectiveInstructionOverride, TargetGuardedAgent, true)
	twoOfThree := Aggregate([]Verdict{attack, attack, {}})
	require.True(t, twoOfThree.IsInjection)
	require.Equal(t, 2, twoOfThree.PositiveVotes)
	require.Equal(t, 3, twoOfThree.Samples)
	require.False(t, twoOfThree.Unanimous)

	oneOfThree := Aggregate([]Verdict{attack, {}, {}})
	require.False(t, oneOfThree.IsInjection)
	require.Equal(t, 1, oneOfThree.PositiveVotes)

	oneOfOne := Aggregate([]Verdict{attack})
	require.True(t, oneOfOne.IsInjection, "samples=1 is the rollback single-call predicate")
}

func TestDetectionPredicateAndTargetDrivenSeverity(t *testing.T) {
	t.Parallel()

	require.True(t, IsInjection(typedVerdict(DirectiveExternalExfiltration, TargetGuardedAgent, true)))
	require.True(t, IsInjection(typedVerdict(DirectiveExternalExfiltration, TargetUnclear, true)))
	require.False(t, IsInjection(typedVerdict(DirectiveExternalExfiltration, TargetOtherContext, true)))
	require.False(t, IsInjection(typedVerdict(DirectiveNone, TargetNone, true)))
	require.False(t, IsInjection(typedVerdict(DirectiveInstructionOverride, TargetGuardedAgent, false)))

	guarded := Aggregate([]Verdict{
		typedVerdict(DirectiveInstructionOverride, TargetGuardedAgent, true),
		typedVerdict(DirectiveInstructionOverride, TargetGuardedAgent, true),
		typedVerdict(DirectiveInstructionOverride, TargetGuardedAgent, true),
	})
	require.Equal(t, SeverityHigh, SeverityFor(guarded, Provenance{Indirect: false}))
	require.Equal(t, ActionBlock, Decide(guarded, SeverityHigh))

	unclear := Aggregate([]Verdict{
		typedVerdict(DirectiveExternalExfiltration, TargetUnclear, true),
		typedVerdict(DirectiveExternalExfiltration, TargetUnclear, true),
		typedVerdict(DirectiveExternalExfiltration, TargetUnclear, true),
	})
	require.Equal(t, SeverityMedium, SeverityFor(unclear, Provenance{Indirect: false}))
	require.Equal(t, ActionWarn, Decide(unclear, SeverityMedium))

	splitUnclear := Aggregate([]Verdict{
		typedVerdict(DirectiveGuardedSecretExtraction, TargetUnclear, true),
		typedVerdict(DirectiveGuardedSecretExtraction, TargetUnclear, true),
		{},
	})
	require.Equal(t, SeverityLow, SeverityFor(splitUnclear, Provenance{Indirect: false}))
	require.Equal(t, ActionLog, Decide(splitUnclear, SeverityLow))
	require.True(t, splitUnclear.IsInjection, "severity and action must never gate a typed detection")
	require.Equal(t, SeverityMedium, SeverityFor(splitUnclear, Provenance{Indirect: true}), "indirect provenance raises but does not gate")
}

func TestRedesignVotesInParallelAndSurfacesAllEligibleDetections(t *testing.T) {
	t.Parallel()

	var response atomic.Int64
	client := &fakeCompletionClient{responder: func(string) string {
		if response.Add(1) <= 2 {
			return `{"directive_kind":"guarded_secret_extraction","target":"unclear","operational":true,"rationale":"typed directive"}`
		}
		return "malformed"
	}}
	engine := newEngine(t, client).WithRedesign(SamplesPerEvent)
	in := req("current event")
	in.Trajectories = []judgemessage.Trajectory{{PriorUserRequest: "inspect output", RecentUntrustedContent: "untrusted context"}}

	results, err := engine.Classify(t.Context(), in)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, promptinjection.LabelInjection, results[0].Label)
	require.InDelta(t, 2.0/3.0, results[0].Score, 0.0001)
	require.Equal(t, DirectiveGuardedSecretExtraction, results[0].Kind)
	require.Equal(t, TargetUnclear, results[0].Target)
	require.Equal(t, string(SeverityLow), results[0].Severity)
	require.Equal(t, string(ActionLog), results[0].Action)
	require.Equal(t, int64(3), client.calls.Load())

	client.mu.Lock()
	payloads := append([]string(nil), client.prompts...)
	requests := append([]gramopenrouter.CompletionRequest(nil), client.requests...)
	client.mu.Unlock()
	require.Len(t, payloads, 3)
	for _, payload := range payloads {
		require.Contains(t, payload, `"prior_user_request":"inspect output"`)
		require.Contains(t, payload, `"recent_untrusted_content":"untrusted context"`)
	}
	require.Len(t, requests, 3)
	for _, request := range requests {
		require.Equal(t, DefaultModel, request.Model)
		require.NotNil(t, request.Temperature)
		require.Zero(t, *request.Temperature)
		require.NotNil(t, request.Reasoning)
		require.Equal(t, DefaultReasoningEffort, request.Reasoning.Effort)
		require.NotNil(t, request.JSONSchema)
		require.Equal(t, VerdictSchema(), request.JSONSchema.Schema)
	}
}

func TestRedesignUsesOneSharedDeadline(t *testing.T) {
	t.Parallel()

	client := &fakeCompletionClient{blockUntilCanceled: true}
	engine := newEngine(t, client).WithRedesign(SamplesPerEvent)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	results, err := engine.Classify(ctx, req("current event"))
	require.NoError(t, err)
	require.Less(t, time.Since(start), 140*time.Millisecond, "parallel samples share the event deadline")
	require.Equal(t, int64(3), client.calls.Load())
	require.Equal(t, promptinjection.LabelSafe, results[0].Label)
}

func TestRedesignSamplesOneMakesOnePhysicalCall(t *testing.T) {
	t.Parallel()

	client := &fakeCompletionClient{responder: func(string) string {
		return `{"directive_kind":"instruction_override","target":"guarded_agent","operational":true,"rationale":"override"}`
	}}
	results, err := newEngine(t, client).WithRedesign(1).Classify(t.Context(), req("current event"))
	require.NoError(t, err)
	require.Equal(t, promptinjection.LabelInjection, results[0].Label)
	require.Equal(t, int64(1), client.calls.Load())
}

func TestRedesignLimiterStoreFailureStillCallsModel(t *testing.T) {
	t.Parallel()

	client := &fakeCompletionClient{responder: func(string) string {
		return `{"directive_kind":"instruction_override","target":"guarded_agent","operational":true,"rationale":"override"}`
	}}
	engine := newEngine(t, client).WithRedesign(SamplesPerEvent)
	engine.limiter = ratelimit.New(nil, "unavailable", ratelimit.Rate{})

	results, err := engine.Classify(t.Context(), req("current event"))
	require.NoError(t, err)
	require.Equal(t, promptinjection.LabelInjection, results[0].Label)
	require.Equal(t, int64(3), client.calls.Load(), "limiter infrastructure failure is not a throttle")
}

func TestRedesignFailOpenReasonsAreBounded(t *testing.T) {
	t.Parallel()

	require.Equal(t, "none", redesignFailureReason(nil, o11y.OutcomeSuccess))
	require.Equal(t, "rate_limited", redesignFailureReason(errRedesignRateLimit, o11y.OutcomeFailure))
	require.Equal(t, "timeout", redesignFailureReason(context.DeadlineExceeded, o11y.OutcomeTimeout))
	require.Equal(t, "malformed", redesignFailureReason(errMalformedVerdict, o11y.OutcomeFailure))
	require.Equal(t, "error", redesignFailureReason(context.Canceled, o11y.OutcomeFailure))
}

func TestZeroRedesignConfigPreservesLegacyRequest(t *testing.T) {
	t.Parallel()

	responder := func(string) string {
		return `{"is_attack":true,"confidence":0.91,"rationale":"legacy verdict"}`
	}
	baselineClient := &fakeCompletionClient{responder: responder}
	baseline, err := newEngine(t, baselineClient).Classify(t.Context(), req("legacy event"))
	require.NoError(t, err)

	gatedClient := &fakeCompletionClient{responder: responder}
	results, err := newEngine(t, gatedClient).ConfigureRedesign(RedesignConfig{Samples: 0, Model: "", Reasoning: ""}).Classify(t.Context(), req("legacy event"))
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, baseline, results, "unset gates preserve the complete legacy verdict")
	require.Equal(t, promptinjection.LabelInjection, results[0].Label)
	require.InDelta(t, 0.91, results[0].Score, 0.0001)
	require.Empty(t, results[0].Kind)
	require.Equal(t, int64(1), baselineClient.calls.Load())
	require.Equal(t, int64(1), gatedClient.calls.Load())

	baselineClient.mu.Lock()
	require.Len(t, baselineClient.requests, 1)
	baselineRequest := baselineClient.requests[0]
	baselineClient.mu.Unlock()
	gatedClient.mu.Lock()
	require.Len(t, gatedClient.requests, 1)
	request := gatedClient.requests[0]
	gatedClient.mu.Unlock()
	require.Equal(t, baselineRequest, request, "unset gates preserve the complete OpenRouter request")
	require.Equal(t, defaultModel, request.Model)
	require.NotNil(t, request.Temperature)
	require.Zero(t, *request.Temperature)
	require.NotNil(t, request.Reasoning)
	require.Equal(t, "none", request.Reasoning.Effort)
	require.NotNil(t, request.JSONSchema)
	require.Equal(t, legacyVerdictSchema(), request.JSONSchema.Schema)
	require.Equal(t, LegacySystemPrompt, gramopenrouter.GetText(request.Messages[0]))
}
