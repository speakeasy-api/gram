package openrouter

import (
	"context"
	"encoding/json"
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

func TestValidVerdictRejectsCrossFieldContradictions(t *testing.T) {
	t.Parallel()

	require.True(t, ValidVerdict(typedVerdict(DirectiveNone, TargetNone, false)))
	require.True(t, ValidVerdict(typedVerdict(DirectiveInstructionOverride, TargetGuardedAgent, true)))
	require.True(t, ValidVerdict(typedVerdict(DirectiveExternalExfiltration, TargetOtherContext, false)))

	require.False(t, ValidVerdict(typedVerdict(DirectiveNone, TargetGuardedAgent, false)))
	require.False(t, ValidVerdict(typedVerdict(DirectiveNone, TargetNone, true)))
	require.False(t, ValidVerdict(typedVerdict(DirectiveInstructionOverride, TargetNone, true)))
}

func TestRedesignedSystemMessageUsesEphemeralCacheControl(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(RedesignedSystemMessage())
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"cache_control"`)
	require.Contains(t, string(encoded), `"ephemeral"`)
}

func TestDetectionPredicateCarriesTypedFields(t *testing.T) {
	t.Parallel()

	require.True(t, IsInjection(typedVerdict(DirectiveExternalExfiltration, TargetGuardedAgent, true)))
	require.True(t, IsInjection(typedVerdict(DirectiveExternalExfiltration, TargetUnclear, true)))
	require.False(t, IsInjection(typedVerdict(DirectiveExternalExfiltration, TargetOtherContext, true)))
	require.False(t, IsInjection(typedVerdict(DirectiveNone, TargetNone, true)))
	require.False(t, IsInjection(typedVerdict(DirectiveInstructionOverride, TargetGuardedAgent, false)))

	stabilized := StabilizeSingle(typedVerdict(DirectiveGuardedSecretExtraction, TargetUnclear, true))
	require.True(t, stabilized.IsInjection)
	require.Equal(t, DirectiveGuardedSecretExtraction, stabilized.DirectiveKind)
	require.Equal(t, TargetUnclear, stabilized.Target)
	require.True(t, stabilized.Operational)
}

func TestOptionalMultiSampleOverrideAggregatesInParallel(t *testing.T) {
	t.Parallel()

	var response atomic.Int64
	client := &fakeCompletionClient{responder: func(string) string {
		if response.Add(1) <= 2 {
			return `{"directive_kind":"guarded_secret_extraction","target":"unclear","operational":true,"rationale":"typed directive"}`
		}
		return "malformed"
	}}
	engine := newEngine(t, client).WithRedesign(3)
	in := req("current event")
	in.Trajectories = []judgemessage.Trajectory{{PriorUserRequest: "inspect output", RecentUntrustedContent: "untrusted context"}}

	results, err := engine.Classify(t.Context(), in)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, promptinjection.LabelInjection, results[0].Label)
	require.Zero(t, results[0].Score)
	require.Equal(t, DirectiveGuardedSecretExtraction, results[0].DirectiveKind)
	require.Equal(t, TargetUnclear, results[0].Target)
	require.True(t, results[0].Operational)
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
	engine := newEngine(t, client).WithRedesign(3)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	results, err := engine.Classify(ctx, req("current event"))
	require.NoError(t, err)
	require.Less(t, time.Since(start), 140*time.Millisecond, "parallel samples share the event deadline")
	require.Equal(t, int64(3), client.calls.Load())
	require.Equal(t, promptinjection.LabelSafe, results[0].Label)
}

func TestTypedPathIsDefaultAndMakesOnePhysicalCall(t *testing.T) {
	t.Parallel()

	client := &fakeCompletionClient{responder: func(string) string {
		return `{"directive_kind":"instruction_override","target":"guarded_agent","operational":true,"rationale":"override"}`
	}}
	results, err := newEngine(t, client).Classify(t.Context(), req("current event"))
	require.NoError(t, err)
	require.Equal(t, promptinjection.LabelInjection, results[0].Label)
	require.Zero(t, results[0].Score, "typed metadata must not overload legacy confidence")
	require.Equal(t, DirectiveInstructionOverride, results[0].DirectiveKind)
	require.Equal(t, TargetGuardedAgent, results[0].Target)
	require.True(t, results[0].Operational)
	require.Equal(t, int64(1), client.calls.Load())

	client.mu.Lock()
	require.Len(t, client.requests, 1)
	request := client.requests[0]
	client.mu.Unlock()
	require.Equal(t, DefaultModel, request.Model)
	require.NotNil(t, request.Reasoning)
	require.Equal(t, DefaultReasoningEffort, request.Reasoning.Effort)
	require.NotNil(t, request.JSONSchema)
	require.Equal(t, VerdictSchema(), request.JSONSchema.Schema)
}

func TestRedesignLimiterStoreFailureStillCallsModel(t *testing.T) {
	t.Parallel()

	client := &fakeCompletionClient{responder: func(string) string {
		return `{"directive_kind":"instruction_override","target":"guarded_agent","operational":true,"rationale":"override"}`
	}}
	engine := newEngine(t, client).WithRedesign(3)
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

func TestLegacyProfileOverrideRestoresBinaryRequest(t *testing.T) {
	t.Parallel()

	responder := func(string) string {
		return `{"is_attack":true,"confidence":0.91,"rationale":"legacy verdict"}`
	}
	legacyClient := &fakeCompletionClient{responder: responder}
	results, err := newEngine(t, legacyClient).Configure(Config{Profile: ProfileLegacy, Samples: 0, Model: "", Reasoning: ""}).Classify(t.Context(), req("legacy event"))
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, promptinjection.LabelInjection, results[0].Label)
	require.InDelta(t, 0.91, results[0].Score, 0.0001)
	require.Empty(t, results[0].DirectiveKind)
	require.Equal(t, int64(1), legacyClient.calls.Load())

	legacyClient.mu.Lock()
	require.Len(t, legacyClient.requests, 1)
	request := legacyClient.requests[0]
	legacyClient.mu.Unlock()
	require.Equal(t, defaultModel, request.Model)
	require.NotNil(t, request.Temperature)
	require.Zero(t, *request.Temperature)
	require.NotNil(t, request.Reasoning)
	require.Equal(t, "none", request.Reasoning.Effort)
	require.NotNil(t, request.JSONSchema)
	require.Equal(t, legacyVerdictSchema(), request.JSONSchema.Schema)
	require.Equal(t, LegacySystemPrompt, gramopenrouter.GetText(request.Messages[0]))
}
