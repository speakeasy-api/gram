// Package riskjudge holds the concrete OpenRouter-backed implementation of the
// prompt-based ("LLM-judge") risk policy evaluator. It lives outside the
// risk_analysis package so that package — which testenv imports — does not pull
// in the OpenRouter client dependency chain (openrouter -> productfeatures ->
// authz), which would otherwise create an import cycle in authz tests.
package riskjudge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/time/rate"

	"github.com/speakeasy-api/gram/server/internal/attr"
	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const (
	// judgeTimeout bounds a single judge call on both the realtime and batch
	// paths.
	judgeTimeout = 10 * time.Second
	// defaultJudgeTemperature keeps verdicts deterministic when a policy does
	// not pin its own temperature.
	defaultJudgeTemperature = 0.0
	// judgeRatePerMin and judgeRateBurst cap how many judge calls a single org
	// can drive per process. Judge calls are billable OpenRouter requests, so
	// this is a cost/abuse guardrail against a thrashing session or runaway
	// batch — tuned generously so only pathological runaway trips it, not normal
	// heavy use (the batch analyzer alone fans out judgeConcurrency=8). These are
	// per-process backstop values: the effective org cap is value × replica count.
	judgeRatePerMin = 600
	judgeRateBurst  = 120
)

const systemPrompt = `You are a security guardrail judge for an AI agent runtime.

You are given an operator-authored policy describing what the operator wants to catch, and a single message from an agent session (typically a tool call). Decide whether the message violates the policy.

Return a JSON object:
- "matched": true if the message violates the policy, false otherwise.
- "confidence": a number in [0,1] for how confident you are.
- "rationale": one short sentence (no more than ~40 words) explaining the decision. Do not echo secrets or raw payloads verbatim.

Judge only against the provided policy. Be precise: do not flag content the policy does not describe. Output ONLY the JSON object, no prose or markdown fences.`

// Judge is the OpenRouter-backed ra.PromptJudge. The judge call mirrors the
// custom-rule suggestion path: strict JSON schema, low temperature, hard
// timeout, OpenRouter object completion.
type Judge struct {
	logger  *slog.Logger
	tracer  trace.Tracer
	metrics *judgeMetrics
	client  openrouter.CompletionClient
	limiter *judgeRateLimiter
}

var _ ra.PromptJudge = (*Judge)(nil)

// New constructs a Judge. A nil client yields a judge whose Evaluate always
// returns nil, so callers can wire it unconditionally.
func New(logger *slog.Logger, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, client openrouter.CompletionClient) *Judge {
	logger = logger.With(attr.SlogComponent("risk-llm-judge"))
	return &Judge{
		logger:  logger,
		tracer:  tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/riskjudge"),
		metrics: newJudgeMetrics(meterProvider, logger),
		client:  client,
		limiter: newJudgeRateLimiter(),
	}
}

// judgeRateLimiter is a per-org, in-memory token-bucket limiter guarding the
// billable judge call. It mirrors the assistant bootstrap limiter: lazy GC of
// idle buckets every 5 minutes, bounded memory without a background goroutine.
type judgeRateLimiter struct {
	mu        sync.Mutex
	state     map[string]*rateLimiterEntry
	limit     rate.Limit
	burst     int
	lastSweep time.Time
}

type rateLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func newJudgeRateLimiter() *judgeRateLimiter {
	return &judgeRateLimiter{
		mu:        sync.Mutex{},
		state:     map[string]*rateLimiterEntry{},
		limit:     rate.Limit(float64(judgeRatePerMin) / 60.0),
		burst:     judgeRateBurst,
		lastSweep: time.Now(),
	}
}

func (l *judgeRateLimiter) allow(org string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Lazy GC: every 5 minutes, drop limiters idle long enough to have refilled
	// to full. Bounds memory across many orgs without paying a goroutine.
	if now.Sub(l.lastSweep) > 5*time.Minute {
		for k, e := range l.state {
			if now.Sub(e.lastSeen) > 5*time.Minute {
				delete(l.state, k)
			}
		}
		l.lastSweep = now
	}

	e, ok := l.state[org]
	if !ok {
		e = &rateLimiterEntry{limiter: rate.NewLimiter(l.limit, l.burst), lastSeen: now}
		l.state[org] = e
	}
	e.lastSeen = now
	return e.limiter.AllowN(now, 1)
}

// Evaluate runs the judge and returns a non-nil verdict when the message
// violates the policy prompt, or nil when it does not. A nil client or an empty
// prompt/text yields nil. On judge error or timeout the configured fail-mode
// decides: fail-open returns nil (allow), fail-closed returns a verdict.
func (j *Judge) Evaluate(ctx context.Context, in ra.JudgeInput) *ra.JudgeVerdict {
	if j == nil || j.client == nil {
		return nil
	}
	if strings.TrimSpace(in.Prompt) == "" || strings.TrimSpace(in.Text) == "" {
		return nil
	}

	ctx, span := j.tracer.Start(ctx, "risk.judge.evaluate", trace.WithAttributes(
		attr.OrganizationID(in.OrgID),
		attr.ProjectID(in.ProjectID),
	))
	defer span.End()

	// Org-scoped guardrail on the billable judge call. A throttled call is
	// treated like a judge error: the policy's fail-mode decides the verdict.
	if !j.limiter.allow(in.OrgID, time.Now()) {
		j.metrics.RecordRateLimited(ctx, in.OrgID)
		span.SetAttributes(attribute.Bool("risk.judge.rate_limited", true))
		j.logger.WarnContext(ctx, "llm judge rate limited",
			attr.SlogOrganizationID(in.OrgID),
		)
		if in.Config.FailOpen {
			return nil
		}
		return &ra.JudgeVerdict{
			Confidence: 0,
			Rationale:  "Policy judge was rate limited; flagged by fail-closed policy.",
		}
	}

	start := time.Now()
	matched, confidence, rationale, err := j.call(ctx, in)
	j.metrics.RecordEvaluation(ctx, in.OrgID, o11y.OutcomeFromError(err), time.Since(start))
	if err != nil {
		span.RecordError(err)
		j.logger.WarnContext(ctx, "llm judge call failed",
			attr.SlogError(err),
			attr.SlogOrganizationID(in.OrgID),
		)
		if in.Config.FailOpen {
			return nil
		}
		return &ra.JudgeVerdict{
			Confidence: 0,
			Rationale:  "Policy judge was unavailable; flagged by fail-closed policy.",
		}
	}
	if !matched {
		return nil
	}
	j.metrics.RecordConfidence(ctx, in.OrgID, confidence)
	span.SetAttributes(
		attribute.Bool("risk.judge.matched", true),
		attribute.Float64("risk.judge.confidence", confidence),
	)
	return &ra.JudgeVerdict{
		Confidence: confidence,
		Rationale:  strings.TrimSpace(rationale),
	}
}

func (j *Judge) call(ctx context.Context, in ra.JudgeInput) (matched bool, confidence float64, rationale string, err error) {
	strict := true
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"matched":    map[string]any{"type": "boolean"},
			"confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
			"rationale":  map[string]any{"type": "string", "maxLength": 500},
		},
		"required":             []string{"matched", "confidence", "rationale"},
		"additionalProperties": false,
	}
	jsonSchema := or.ChatJSONSchemaConfig{
		Name:        "risk_policy_judge_verdict",
		Schema:      schema,
		Description: nil,
		Strict:      optionalnullable.From(&strict),
	}

	temperature := defaultJudgeTemperature
	if in.Config.Temperature != nil {
		temperature = *in.Config.Temperature
	}

	userMessage := fmt.Sprintf("Policy:\n%s\n\nMessage to evaluate:\n%s", in.Prompt, in.Text)

	// Empty model selects the judge default (a cheap, fast model) rather than
	// inheriting the system-wide chat default, which is expensive for what is a
	// high-volume per-message binary classification.
	model := in.Config.Model
	if model == "" {
		model = ra.DefaultJudgeModel
	}

	callCtx, cancel := context.WithTimeout(ctx, judgeTimeout)
	defer cancel()

	response, err := j.client.GetObjectCompletion(callCtx, openrouter.ObjectCompletionRequest{
		OrgID:          in.OrgID,
		ProjectID:      in.ProjectID,
		Model:          model,
		SystemPrompt:   systemPrompt,
		Prompt:         userMessage,
		Temperature:    &temperature,
		UsageSource:    billing.ModelUsageSourceGram,
		UserID:         "",
		ExternalUserID: "",
		HTTPMetadata:   nil,
		JSONSchema:     &jsonSchema,
	})
	if err != nil {
		return false, 0, "", fmt.Errorf("openrouter object completion: %w", err)
	}
	if response == nil || response.Message == nil {
		return false, 0, "", fmt.Errorf("empty completion response")
	}
	raw := strings.TrimSpace(openrouter.GetText(*response.Message))
	if raw == "" {
		return false, 0, "", fmt.Errorf("empty completion content")
	}

	var verdict struct {
		Matched    bool    `json:"matched"`
		Confidence float64 `json:"confidence"`
		Rationale  string  `json:"rationale"`
	}
	if err := json.Unmarshal([]byte(raw), &verdict); err != nil {
		return false, 0, "", fmt.Errorf("parse judge response: %w", err)
	}
	return verdict.Matched, max(0, min(1, verdict.Confidence)), verdict.Rationale, nil
}
