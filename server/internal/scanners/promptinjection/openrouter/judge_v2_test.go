package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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

func TestTypedSystemMessageUsesEphemeralCacheControl(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(TypedSystemMessage())
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
	engine := newEngine(t, client).WithTypedSamples(3)
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

func TestTypedSamplesUseOneSharedDeadline(t *testing.T) {
	t.Parallel()

	client := &fakeCompletionClient{blockUntilCanceled: true}
	engine := newEngine(t, client).WithTypedSamples(3)
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

func TestTypedLimiterStoreFailureStillCallsModel(t *testing.T) {
	t.Parallel()

	client := &fakeCompletionClient{responder: func(string) string {
		return `{"directive_kind":"instruction_override","target":"guarded_agent","operational":true,"rationale":"override"}`
	}}
	engine := newEngine(t, client).WithTypedSamples(3)
	engine.limiter = ratelimit.New(nil, "unavailable", ratelimit.Rate{})

	results, err := engine.Classify(t.Context(), req("current event"))
	require.NoError(t, err)
	require.Equal(t, promptinjection.LabelInjection, results[0].Label)
	require.Equal(t, int64(3), client.calls.Load(), "limiter infrastructure failure is not a throttle")
}

func TestTypedFailOpenReasonsAreBounded(t *testing.T) {
	t.Parallel()

	require.Equal(t, "none", typedFailureReason(nil, o11y.OutcomeSuccess))
	require.Equal(t, "rate_limited", typedFailureReason(errTypedRateLimit, o11y.OutcomeFailure))
	require.Equal(t, "timeout", typedFailureReason(context.DeadlineExceeded, o11y.OutcomeTimeout))
	require.Equal(t, "malformed", typedFailureReason(errMalformedVerdict, o11y.OutcomeFailure))
	require.Equal(t, "error", typedFailureReason(context.Canceled, o11y.OutcomeFailure))
}

func TestTypedContextObservabilityIncludesSuppressedVerdict(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		require.NoError(t, tracerProvider.Shutdown(context.Background()))
		require.NoError(t, meterProvider.Shutdown(context.Background()))
	})

	client := &fakeCompletionClient{responder: func(string) string {
		return `{"directive_kind":"instruction_override","target":"other_context","operational":true,"rationale":"archived directive"}`
	}}
	engine := New(testenv.NewLogger(t), tracerProvider, meterProvider, client, testJudgeLimiter(t))
	in := req("both context fields", "prior only", "recent only", "no context")
	in.Trajectories = []judgemessage.Trajectory{
		{
			PriorUserRequest:       strings.Repeat("界", judgemessage.MaxTrajectoryBodyRunes+1),
			RecentUntrustedContent: "recent untrusted context",
		},
		{PriorUserRequest: "prior context", RecentUntrustedContent: ""},
		{PriorUserRequest: "", RecentUntrustedContent: "recent context"},
		{PriorUserRequest: "", RecentUntrustedContent: ""},
	}

	results, err := engine.Classify(t.Context(), in)
	require.NoError(t, err)
	require.Len(t, results, 4)
	for _, result := range results {
		require.Equal(t, promptinjection.LabelSafe, result.Label, "other-context verdicts do not emit a finding")
	}

	var eventSpan sdktrace.ReadOnlySpan
	for _, span := range recorder.Ended() {
		if span.Name() != "risk.prompt_injection.classify.typed_event" {
			continue
		}
		for _, kv := range span.Attributes() {
			if kv.Key == spanAttrPriorLen && kv.Value.AsInt64() == int64(judgemessage.MaxTrajectoryBodyRunes+1) {
				eventSpan = span
			}
		}
	}
	require.NotNil(t, eventSpan)
	spanAttrs := make(map[attribute.Key]attribute.Value)
	for _, kv := range eventSpan.Attributes() {
		spanAttrs[kv.Key] = kv.Value
	}
	require.True(t, spanAttrs[spanAttrContextPresent].AsBool())
	require.True(t, spanAttrs[spanAttrPriorPresent].AsBool())
	require.True(t, spanAttrs[spanAttrRecentPresent].AsBool())
	require.True(t, spanAttrs[spanAttrPriorTruncated].AsBool())
	require.False(t, spanAttrs[spanAttrRecentTruncated].AsBool())
	require.Equal(t, int64(judgemessage.MaxTrajectoryBodyRunes+1), spanAttrs[spanAttrPriorLen].AsInt64())
	require.Equal(t, int64(24), spanAttrs[spanAttrRecentLen].AsInt64())
	require.Equal(t, DirectiveInstructionOverride, spanAttrs[spanAttrDirectiveKind].AsString())
	require.Equal(t, TargetOtherContext, spanAttrs[spanAttrTarget].AsString())
	require.True(t, spanAttrs[spanAttrOperational].AsBool())
	require.False(t, spanAttrs[spanAttrFindingSurfaced].AsBool())

	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &collected))

	coverage := counterPoints(t, collected, meterTypedContextCoverage)
	require.Len(t, coverage, 4)
	coverageStates := make(map[string]int64, 4)
	for _, point := range coverage {
		attrs := metricAttrs(point.Attributes)
		key := fmt.Sprintf(
			"%s/%t/%t",
			attrs["coverage"].AsString(),
			attrs["prior_user_request_present"].AsBool(),
			attrs["recent_untrusted_content_present"].AsBool(),
		)
		coverageStates[key] = point.Value
	}
	require.Equal(t, map[string]int64{
		"both/true/true":      1,
		"either/true/false":   1,
		"either/false/true":   1,
		"neither/false/false": 1,
	}, coverageStates)

	fields := counterPoints(t, collected, meterTypedContextFields)
	var priorTruncations int64
	var recentTruncations int64
	for _, point := range fields {
		attrs := metricAttrs(point.Attributes)
		if !attrs["truncated"].AsBool() {
			continue
		}
		switch attrs["field"].AsString() {
		case "prior_user_request":
			priorTruncations += point.Value
		case "recent_untrusted_content":
			recentTruncations += point.Value
		}
	}
	require.Equal(t, int64(1), priorTruncations)
	require.Zero(t, recentTruncations)

	verdicts := counterPoints(t, collected, meterTypedVerdicts)
	require.Len(t, verdicts, 2)
	var contextVerdicts int64
	var contextAbsentVerdicts int64
	for _, point := range verdicts {
		attrs := metricAttrs(point.Attributes)
		require.Equal(t, DirectiveInstructionOverride, attrs["directive_kind"].AsString())
		require.Equal(t, TargetOtherContext, attrs["target"].AsString())
		require.True(t, attrs["operational"].AsBool())
		require.False(t, attrs["finding_surfaced"].AsBool())
		if attrs["session_context_present"].AsBool() {
			contextVerdicts += point.Value
		} else {
			contextAbsentVerdicts += point.Value
		}
	}
	require.Equal(t, int64(3), contextVerdicts)
	require.Equal(t, int64(1), contextAbsentVerdicts)
}

func counterPoints(t *testing.T, collected metricdata.ResourceMetrics, name string) []metricdata.DataPoint[int64] {
	t.Helper()
	for _, scope := range collected.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != name {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			return sum.DataPoints
		}
	}
	require.Failf(t, "metric not found", "missing metric %q", name)
	return nil
}

func metricAttrs(set attribute.Set) map[string]attribute.Value {
	attrs := make(map[string]attribute.Value, set.Len())
	for _, kv := range set.ToSlice() {
		attrs[string(kv.Key)] = kv.Value
	}
	return attrs
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
	require.Equal(t, LegacyModel, request.Model)
	require.NotNil(t, request.Temperature)
	require.Zero(t, *request.Temperature)
	require.NotNil(t, request.Reasoning)
	require.Equal(t, "none", request.Reasoning.Effort)
	require.NotNil(t, request.JSONSchema)
	require.Equal(t, legacyVerdictSchema(), request.JSONSchema.Schema)
	require.Equal(t, LegacySystemPrompt, gramopenrouter.GetText(request.Messages[0]))
}
