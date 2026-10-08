package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	piopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptinjection/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	typesafe "github.com/speakeasy-api/gram/server/internal/thirdparty/typesafedecisions"
	meternoop "go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

const (
	// finishReasonContentFilter is the finish_reason of a completion the
	// provider's safety classifier refused.
	finishReasonContentFilter = "content_filter"

	// maxVerdictAttempts bounds confirmer calls per request while the model
	// refuses or returns no valid verdict. Three matches the evaluation
	// harness, so refusals score as they did in the report.
	maxVerdictAttempts = 3

	// maxCaseAttempts bounds how often a case that failed open on a
	// transient provider error (throttling, server error, timeout) runs
	// again before it is scored.
	maxCaseAttempts = 4

	// caseRetryBaseDelay is the wait before the second case attempt; it
	// doubles for each later one (5s, 10s, 20s), outside the production
	// deadlines, so a throttled key can recover.
	caseRetryBaseDelay = 5 * time.Second
)

// scanCascade exercises the production orchestration and payloads. The worker
// pool bounds the number of in-flight cases. Refused or malformed
// confirmations are asked again, and a case that failed open on a transient
// provider error runs again, before it is scored.
func scanCascade(ctx context.Context, opts options, key string, corpus []labeledCase) ([][]scanners.Finding, evaluationStats, error) {
	tracer, meter := tracenoop.NewTracerProvider(), meternoop.NewMeterProvider()
	logger := slog.New(slog.DiscardHandler)
	policy := guardian.NewDefaultPolicy(tracer)
	jev := typesafe.New(policy.PooledClient(), func(context.Context, string) (string, error) { return key, nil })
	client := newOpenRouterClient(key)
	results := make([][]scanners.Finding, len(corpus))
	observations := make([]decisionObservation, len(corpus))
	missed := make([]bool, len(corpus))
	confirmations := make([]int, len(corpus))
	refusals := make([]int, len(corpus))
	fallbacks := make([]int, len(corpus))
	refused := make([]bool, len(corpus))
	failed := make([]bool, len(corpus))
	firstUnavailable := make([]bool, len(corpus))
	sem := make(chan struct{}, opts.judgeConcurrency)
	var wg sync.WaitGroup
	for i, row := range corpus {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			observation := &observations[i]
			completion := &observedCompletion{
				CompletionClient: client, observation: observation, calls: &confirmations[i], refusals: &refusals[i],
				fallbacks: &fallbacks[i], refused: &refused[i], refusalFallback: opts.refusalFallback,
			}
			prefilter := &observedPrefilter{Evaluator: jev, observation: observation}
			load := func(_ context.Context, _, _ string, target judgemessage.Message) (judgemessage.Window, error) {
				if row.Window == nil {
					return judgemessage.Window{Messages: []judgemessage.Payload{judgemessage.RenderPayload(target)}, TargetIndex: 0}, nil
				}
				window := *row.Window
				if len(window.Messages) > 5 || window.TargetIndex < 0 || window.TargetIndex >= len(window.Messages) {
					return judgemessage.Window{}, fmt.Errorf("invalid corpus context window")
				}
				window.Messages = append([]judgemessage.Payload(nil), window.Messages...)
				window.Messages[window.TargetIndex] = judgemessage.RenderPayload(target)
				return window, nil
			}
			cascade := piopenrouter.NewCascade(logger, tracer, meter, completion, prefilter, load)
			scanner := promptinjection.NewScanner(logger, cascade.Classify)
			result, verdict, err, initialUnavailable := runCascadeCase(ctx, observation, func() (scanners.Result, promptinjection.Result, error) {
				refused[i] = false
				return scanner.ScanStrictWithVerdict(ctx, row.Text, benchOrgID, benchProjectID, "", row.judgeMessage(), row.trajectory())
			}, time.Now, waitToRetry)
			firstUnavailable[i] = initialUnavailable
			results[i] = result.Findings
			missed[i] = row.Label == "malicious" && (verdict.Model == typesafe.Model || strings.HasPrefix(verdict.Model, typesafe.Model+"-")) && verdict.Completed && verdict.Label == promptinjection.LabelSafe
			failed[i] = err != nil || verdict.Label == promptinjection.LabelUnavailable
		})
	}
	wg.Wait()
	stats := summarizeEvaluation(observations)
	// A recovered Jev overflow is a failed physical call, not a failed scan.
	stats.FailOpenEvents = 0
	stats.BenchmarkCases = len(corpus)
	for i := range observations {
		if firstUnavailable[i] {
			stats.BenchmarkFirstAttemptUnavailable++
		}
		stats.ConfirmationCalls += confirmations[i]
		stats.ConfirmationRefusals += refusals[i]
		stats.RefusalFallbackCalls += fallbacks[i]
		if refused[i] {
			stats.ConfirmationRefusedEvents++
		}
		if failed[i] {
			stats.FailOpenEvents++
		}
		if missed[i] {
			stats.PrefilterMissedAttacks++
		}
	}
	return results, stats, nil
}

// runCascadeCase measures the complete benchmark case, including failed attempts
// and retry waits. The first attempt includes benchmark-only confirmer retries;
// its availability is not a measurement of the production deadline.
func runCascadeCase(ctx context.Context, observation *decisionObservation, scan func() (scanners.Result, promptinjection.Result, error), now func() time.Time, wait func(context.Context, int) bool) (scanners.Result, promptinjection.Result, error, bool) {
	started := now()
	defer func() { observation.Latency = now().Sub(started) }()
	firstUnavailable := false
	for attempt := 1; ; attempt++ {
		firstCall := len(observation.Calls)
		result, verdict, err := scan()
		unavailable := err != nil || verdict.Label == promptinjection.LabelUnavailable
		if attempt == 1 {
			firstUnavailable = unavailable
		}
		if unavailable && attempt < maxCaseAttempts && slices.ContainsFunc(observation.Calls[firstCall:], transientCallFailure) && wait(ctx, attempt) {
			continue
		}
		if unavailable {
			if err == nil {
				err = promptinjection.ErrNoVerdict
			}
			// Context/metering failures need an event error even if both
			// transports succeeded; do not count them as extra physical calls.
			if len(observation.Calls) > firstCall && observation.Calls[len(observation.Calls)-1].Err == nil {
				observation.Calls[len(observation.Calls)-1].Err = err
			}
		}
		return result, verdict, err, firstUnavailable
	}
}

type observedPrefilter struct {
	typesafe.Evaluator
	observation *decisionObservation
}

func (c *observedPrefilter) Evaluate(ctx context.Context, orgID string, state json.RawMessage, questions map[string]typesafe.Question) (typesafe.Result, error) {
	start := time.Now()
	result, err := c.Evaluator.Evaluate(ctx, orgID, state, questions)
	c.observation.Calls = append(c.observation.Calls, callObservation{Latency: time.Since(start), PromptTokens: result.InputTokens, CompletionTokens: result.OutputTokens, CostUSD: result.CostUSD, Err: err})
	if err != nil {
		return result, fmt.Errorf("observe cascade call: %w", err)
	}
	return result, nil
}

// transientCallFailure reports whether a failed physical call may succeed
// later: throttling, a server error, a timeout or a transport failure, not a
// rejected request or an exhausted credit balance.
func transientCallFailure(call callObservation) bool {
	err := call.Err
	if err == nil || errors.Is(err, typesafe.ErrContextLengthExceeded) || errors.Is(err, typesafe.ErrUnavailable) || openrouter.IsPermanentError(err) {
		return false
	}
	if status, ok := errors.AsType[*typesafe.StatusError](err); ok {
		return status.StatusCode == http.StatusRequestTimeout || status.StatusCode == http.StatusTooManyRequests || status.StatusCode >= http.StatusInternalServerError
	}
	return true
}

// waitToRetry waits before case attempt attempt+1 and reports whether the
// run can continue.
func waitToRetry(ctx context.Context, attempt int) bool {
	timer := time.NewTimer(caseRetryBaseDelay << (attempt - 1))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

type observedCompletion struct {
	openrouter.CompletionClient
	observation *decisionObservation
	calls       *int
	// refusals counts safety-classifier refusals (finish_reason
	// content_filter); fallbacks counts calls to the refusal fallback model.
	refusals  *int
	fallbacks *int

	// refused records whether the confirmation model's last request ended
	// refused after every attempt.
	refused *bool

	// refusalFallback is false to skip the refusal fallback model, scoring
	// a refused confirmation as a refusal without another call.
	refusalFallback bool
}

// GetCompletion asks again while the model refuses or returns no valid
// verdict, up to maxVerdictAttempts calls, as the evaluation harness does.
func (c *observedCompletion) GetCompletion(ctx context.Context, req openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	if req.Model == piopenrouter.RefusalFallbackModel && !c.refusalFallback {
		var skipped openrouter.CompletionResponse
		skipped.Model = req.Model
		skipped.FinishReason = new(finishReasonContentFilter)
		return &skipped, nil
	}
	for attempt := 1; ; attempt++ {
		result, err := c.complete(ctx, req)
		if err != nil || hasVerdict(result) || attempt == maxVerdictAttempts || ctx.Err() != nil {
			if req.Model != piopenrouter.RefusalFallbackModel {
				*c.refused = err == nil && isRefusal(result)
			}
			return result, err
		}
	}
}

func isRefusal(result *openrouter.CompletionResponse) bool {
	return result != nil && result.FinishReason != nil && *result.FinishReason == finishReasonContentFilter
}

// hasVerdict reports whether a completion carries a verdict the production
// judge accepts: not refused or truncated, with every field present and valid.
func hasVerdict(result *openrouter.CompletionResponse) bool {
	if result == nil || result.Message == nil || isRefusal(result) || (result.FinishReason != nil && *result.FinishReason == openrouter.FinishReasonLength) {
		return false
	}
	var verdict struct {
		DirectiveKind *string `json:"directive_kind"`
		Target        *string `json:"target"`
		Operational   *bool   `json:"operational"`
		Rationale     *string `json:"rationale"`
	}
	raw := strings.TrimSpace(openrouter.GetText(*result.Message))
	if err := json.Unmarshal([]byte(raw), &verdict); err != nil || verdict.DirectiveKind == nil || verdict.Target == nil || verdict.Operational == nil || verdict.Rationale == nil {
		return false
	}
	return piopenrouter.ValidVerdict(piopenrouter.Verdict{
		DirectiveKind: *verdict.DirectiveKind,
		Target:        *verdict.Target,
		Operational:   *verdict.Operational,
		Rationale:     *verdict.Rationale,
	})
}

func (c *observedCompletion) complete(ctx context.Context, req openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	*c.calls++
	if req.Model == piopenrouter.RefusalFallbackModel {
		*c.fallbacks++
	}
	start := time.Now()
	result, err := c.CompletionClient.GetCompletion(ctx, req)
	if isRefusal(result) {
		*c.refusals++
	}
	call := callObservation{Latency: time.Since(start), PromptTokens: 0, CompletionTokens: 0, CostUSD: 0, Err: err}
	if result != nil {
		call.PromptTokens, call.CompletionTokens = result.Usage.PromptTokens, result.Usage.CompletionTokens
		if result.Usage.Cost != nil {
			call.CostUSD = *result.Usage.Cost
		}
	}
	c.observation.Calls = append(c.observation.Calls, call)
	if err != nil {
		return result, fmt.Errorf("observe cascade call: %w", err)
	}
	return result, nil
}
