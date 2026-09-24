//go:build policy_cascade_eval

package openrouter

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/typesafe"
)

type evaluationWindow struct{ window judgemessage.Window }

func (w evaluationWindow) Load(context.Context, string, string, judgemessage.Message) (judgemessage.Window, error) {
	return w.window, nil
}

// TestCascadeLiveEvaluation is an opt-in, synthetic evaluation of the production
// baseline and cascade. The build tag explicitly opts into paid provider calls.
//
//nolint:paralleltest // Sequential calls bound provider spend and keep aggregate reporting deterministic.
func TestCascadeLiveEvaluation(t *testing.T) {
	key := os.Getenv("OPENROUTER_DEV_KEY")
	if key == "" || key == "unset" {
		key = os.Getenv("OPENROUTER_API_KEY")
	}
	require.NotEmpty(t, key, "an OpenRouter development key is required")
	require.NotEqual(t, "unset", key)
	raw, err := os.ReadFile("testdata/cascade_cases.json")
	require.NoError(t, err)
	var cases []struct {
		ID          string `json:"id"`
		Policy      string `json:"policy"`
		Expected    bool   `json:"expected"`
		TargetIndex int    `json:"target_index"`
		Messages    []struct {
			Type string `json:"type"`
			Tool string `json:"tool"`
			Body string `json:"body"`
		} `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(raw, &cases))
	logger := testenv.NewLogger(t)
	tracer := testenv.NewTracerProvider(t)
	meter := testenv.NewMeterProvider(t)
	policy := guardian.NewDefaultPolicy(tracer)
	provisioner := openrouter.NewDevelopment(key)
	completion := openrouter.NewUnifiedClient(logger, policy, provisioner, &openrouter.PlatformKeyResolver{Provisioner: provisioner}, nil, nil, nil, nil)
	prefilter := typesafe.New(policy.PooledClient(), func(context.Context, string) (string, error) { return key, nil })
	type totals struct {
		fp, fn, tp, tn, errors, escalated int
		cost                              float64
		duration                          time.Duration
	}
	baselineTotal, cascadeTotal := totals{}, totals{}
	for _, tc := range cases {
		t.Run(tc.ID, func(t *testing.T) {
			require.LessOrEqual(t, len(tc.Messages), 5)
			msgs := make([]judgemessage.Message, 0, len(tc.Messages))
			window := judgemessage.Window{Messages: make([]judgemessage.Payload, 0, len(tc.Messages)), TargetIndex: tc.TargetIndex}
			for _, m := range tc.Messages {
				msg := judgemessage.New(m.Type, m.Tool, m.Body)
				msgs = append(msgs, msg)
				window.Messages = append(window.Messages, judgemessage.RenderPayload(msg))
			}
			input := promptpolicy.Input{OrgID: "org-example", ProjectID: "00000000-0000-0000-0000-000000000001", UserID: "", Prompt: tc.Policy, Message: msgs[tc.TargetIndex], Config: promptpolicy.Config{Temperature: nil, FailOpen: true}}
			judge := New(logger, tracer, meter, completion, testJudgeLimiter(t))
			cascade := &Cascade{judge: judge, prefilter: prefilter, windows: evaluationWindow{window: window}, enabled: func(context.Context, string, string) (bool, error) { return true, nil }, slots: make(chan struct{}, 1)}
			for _, run := range []struct {
				name     string
				evaluate promptpolicy.Evaluator
				total    *totals
			}{{name: "baseline", evaluate: judge.Evaluate, total: &baselineTotal}, {name: "cascade", evaluate: cascade.Evaluate, total: &cascadeTotal}} {
				started := time.Now()
				verdict, err := run.evaluate(t.Context(), input)
				elapsed := time.Since(started)
				run.total.duration += elapsed
				if err != nil || verdict == nil || !verdict.Completed {
					run.total.errors++
					t.Logf("%s no verdict (%s)", run.name, elapsed)
					continue
				}
				run.total.cost += verdict.CostUSD
				if verdict.Model == CascadeModel {
					run.total.escalated++
				}
				switch {
				case verdict.Matched && tc.Expected:
					run.total.tp++
				case verdict.Matched:
					run.total.fp++
				case tc.Expected:
					run.total.fn++
				default:
					run.total.tn++
				}
				t.Logf("%s expected=%t matched=%t latency=%s cost_usd=%.6f model=%s", run.name, tc.Expected, verdict.Matched, elapsed, verdict.CostUSD, verdict.Model)
			}
		})
	}
	t.Logf("baseline: TP=%d FP=%d TN=%d FN=%d errors=%d cost_usd=%.6f duration=%s", baselineTotal.tp, baselineTotal.fp, baselineTotal.tn, baselineTotal.fn, baselineTotal.errors, baselineTotal.cost, baselineTotal.duration)
	t.Logf("cascade: TP=%d FP=%d TN=%d FN=%d errors=%d escalated=%d/%d cost_usd=%.6f duration=%s", cascadeTotal.tp, cascadeTotal.fp, cascadeTotal.tn, cascadeTotal.fn, cascadeTotal.errors, cascadeTotal.escalated, len(cases), cascadeTotal.cost, cascadeTotal.duration)
	require.Zero(t, baselineTotal.errors, "baseline provider calls must complete for a meaningful comparison")
	require.Zero(t, cascadeTotal.errors, "cascade provider calls must complete for a meaningful comparison")
}
