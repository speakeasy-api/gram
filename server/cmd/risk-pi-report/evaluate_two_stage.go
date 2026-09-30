package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

// scanCascade exercises the production orchestration and payloads. The worker
// pool bounds the number of in-flight cases.
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
	failed := make([]bool, len(corpus))
	sem := make(chan struct{}, opts.judgeConcurrency)
	var wg sync.WaitGroup
	for i, row := range corpus {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			start := time.Now()
			observation := &observations[i]
			completion := &observedCompletion{CompletionClient: client, observation: observation, calls: &confirmations[i], refusals: &refusals[i], fallbacks: &fallbacks[i]}
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
			result, verdict, err := scanner.ScanStrictWithVerdict(ctx, row.Text, benchOrgID, benchProjectID, "", row.judgeMessage(), row.trajectory())
			results[i] = result.Findings
			missed[i] = row.Label == "malicious" && (verdict.Model == typesafe.Model || strings.HasPrefix(verdict.Model, typesafe.Model+"-")) && verdict.Completed && verdict.Label == promptinjection.LabelSafe
			if err != nil || verdict.Label == promptinjection.LabelUnavailable {
				failed[i] = true
				if err == nil {
					err = promptinjection.ErrNoVerdict
				}
				// Context/metering failures need an event error even if both
				// transports succeeded; do not count them as extra physical calls.
				if len(observation.Calls) > 0 && observation.Calls[len(observation.Calls)-1].Err == nil {
					observation.Calls[len(observation.Calls)-1].Err = err
				}
			}
			observation.Latency = time.Since(start)
		})
	}
	wg.Wait()
	stats := summarizeEvaluation(observations)
	// A recovered Jev overflow is a failed physical call, not a failed scan.
	stats.FailOpenEvents = 0
	for i := range observations {
		stats.ConfirmationCalls += confirmations[i]
		stats.ConfirmationRefusals += refusals[i]
		stats.RefusalFallbackCalls += fallbacks[i]
		if failed[i] {
			stats.FailOpenEvents++
		}
		if missed[i] {
			stats.PrefilterMissedAttacks++
		}
	}
	return results, stats, nil
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

type observedCompletion struct {
	openrouter.CompletionClient
	observation *decisionObservation
	calls       *int
	// refusals counts safety-classifier refusals (finish_reason
	// content_filter); fallbacks counts calls to the refusal fallback model.
	refusals  *int
	fallbacks *int
}

func (c *observedCompletion) GetCompletion(ctx context.Context, req openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	*c.calls++
	if req.Model == piopenrouter.RefusalFallbackModel {
		*c.fallbacks++
	}
	start := time.Now()
	result, err := c.CompletionClient.GetCompletion(ctx, req)
	if result != nil && result.FinishReason != nil && *result.FinishReason == "content_filter" {
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
