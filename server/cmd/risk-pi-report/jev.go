package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/jev"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	piopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptinjection/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/typesafe"
)

// jevInputCostPerToken mirrors typesafe.Model's published per-input-token
// price; output is free. Kept local since the report only needs it for the
// cost column, not for any runtime decision.
const jevInputCostPerToken = 0.042 / 1_000_000

// scanJevMode evaluates Jev (TypeSafe) over the corpus as a shadow candidate
// for the L1 prompt-injection judge. It builds the exact same prepared
// evidence the production judgeshadow.Publisher sends (PrepareJudgePayload),
// so the numbers reflect Jev's raw accuracy on every case, not a sampled
// shadow slice.
func scanJevMode(ctx context.Context, opts options, corpus []labeledCase) (modeSummary, [][]scanners.Finding, error) {
	policy := guardian.NewDefaultPolicy(tracenoop.NewTracerProvider())
	var evaluator typesafe.Evaluator
	if opts.jevOpenRouter {
		apiKey := os.Getenv("OPENROUTER_DEV_KEY")
		if apiKey == "" || apiKey == "unset" {
			return modeSummary{}, nil, fmt.Errorf("OPENROUTER_DEV_KEY not set")
		}
		evaluator = typesafe.NewOpenRouterClient(policy.PooledClient(), apiKey)
	} else {
		apiKey := os.Getenv("TYPESAFE_API_KEY")
		if apiKey == "" || apiKey == "unset" {
			return modeSummary{}, nil, fmt.Errorf("TYPESAFE_API_KEY not set")
		}
		evaluator = typesafe.New(policy.PooledClient(), apiKey)
	}

	fmt.Fprintf(os.Stderr, "judging %d cases with jev (concurrency=%d, via_openrouter=%v)\n", len(corpus), opts.judgeConcurrency, opts.jevOpenRouter)
	judge := jev.New(evaluator)
	findings, eval, err := scanJev(ctx, opts, judge, corpus)
	if err != nil {
		return modeSummary{}, nil, err
	}

	mode := summarizeFindings("jev", corpus, findings)
	mode.Evaluation = eval
	empty := make([][]scanners.Finding, len(corpus))
	mode.NewFalsePositives = changedExamples(corpus, empty, findings, "benign", 500)
	mode.RecoveredTruePositive = changedExamples(corpus, empty, findings, "malicious", 500)
	mode.MissedAttacks = missedExamples(corpus, findings, 500)
	return mode, findings, nil
}

// scanJev runs Jev for every corpus row and records positive verdicts. A
// match is any question probability at or above jev.Threshold, matching
// judgeshadow.Handler's outcome derivation exactly.
func scanJev(ctx context.Context, opts options, judge *jev.Judge, corpus []labeledCase) ([][]scanners.Finding, evaluationStats, error) {
	out := make([][]scanners.Finding, len(corpus))
	ruleID, description := promptinjection.Describe()

	sem := make(chan struct{}, opts.judgeConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var done int
	observations := make([]decisionObservation, len(corpus))

	for i := range corpus {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()

			text := corpus[i].Text
			msg := corpus[i].judgeMessage()
			state, _ := piopenrouter.PrepareJudgePayload(msg, corpus[i].trajectory())

			decisionCtx, cancel := context.WithTimeout(ctx, piopenrouter.JudgeTimeout)
			start := time.Now()
			result, err := judge.Evaluate(decisionCtx, promptinjection.Source, state)
			cancel()
			latency := time.Since(start)

			call := callObservation{Latency: latency, PromptTokens: 0, CompletionTokens: 0, CostUSD: 0, Err: err}
			if err == nil {
				call.PromptTokens = result.InputTokens
				call.CompletionTokens = result.OutputTokens
				call.CostUSD = float64(result.InputTokens) * jevInputCostPerToken
			}

			mu.Lock()
			defer mu.Unlock()
			done++
			if done%20 == 0 || done == len(corpus) {
				fmt.Fprintf(os.Stderr, "\r  jev %d/%d", done, len(corpus))
			}
			observations[i] = decisionObservation{Calls: []callObservation{call}, Latency: latency}
			if err != nil {
				return
			}

			matchedQuestion, maxProbability := "", 0.0
			for id, probability := range result.Probabilities {
				if probability > maxProbability {
					matchedQuestion, maxProbability = id, probability
				}
			}
			if maxProbability < jev.Threshold {
				return
			}
			out[i] = append(out[i], scanners.Finding{
				RuleID:      ruleID,
				Description: description,
				Match:       text,
				StartPos:    0,
				EndPos:      len(text),
				Source:      promptinjection.Source,
				// Report-only score keeps the false-positive/missed-attack
				// example lists sortable, same convention as the OpenRouter mode.
				Confidence:          maxProbability,
				Tags:                []string{"jev", "layer-1-shadow", "semantic", "question:" + matchedQuestion},
				DeadLetterReason:    "",
				McpLookupToolCallID: "",
				SpanGroupKey:        "",
				Field:               "",
				Path:                "",
			})
		}(i)
	}
	wg.Wait()
	fmt.Fprintln(os.Stderr)
	return out, summarizeEvaluation(observations), nil
}
