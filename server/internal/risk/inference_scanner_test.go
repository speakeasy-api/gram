package risk_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/risk"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

// These exercise the real enforcement scanner and detector adapters with stored
// policies/grants. Only the external analyzer or judge is substituted.
func TestScanner_InferencePromptInjectionCompleteness(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		verdict    promptinjection.Result
		err        error
		incomplete bool
	}{
		{name: "provider error", err: errors.New("classifier unavailable"), incomplete: true},
		{name: "inner deadline", err: context.DeadlineExceeded, incomplete: true},
		{name: "unavailable verdict", verdict: promptinjection.Result{Label: promptinjection.LabelUnavailable}, incomplete: true},
		{name: "incomplete verdict", verdict: promptinjection.Result{Label: promptinjection.LabelSafe}, incomplete: true},
		{name: "completed clean", verdict: promptinjection.Result{Label: promptinjection.LabelSafe, Completed: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestRiskService(t)
			insertRealtimeBlockPolicy(t, ti, ctx, "inference injection", []string{"prompt_injection"}, nil)
			var calls atomic.Int32
			classifier := promptinjection.Classifier(func(context.Context, promptinjection.Request) ([]promptinjection.Result, error) {
				calls.Add(1)
				return []promptinjection.Result{tc.verdict}, tc.err
			})
			scanner, err := risk.NewScanner(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn, newTestCustomRuleAnalyzer(t, ti.conn), nil, promptinjection.NewScanner(testenv.NewLogger(t), classifier), nil, &feature.InMemory{}, testCELEngine(t), metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()))
			require.NoError(t, err)
			auth, _ := contextvalues.GetAuthContext(ctx)
			req := realtimeScanRequest(auth.ActiveOrganizationID, *auth.ProjectID, auth.UserID, "untrusted tool output", message.ToolResponse, "read_file")
			result, err := scanner.ScanForEnforcement(ctx, req)
			require.NoError(t, err)
			require.Nil(t, result)
			outcome, err := scanner.ScanForInferenceEnforcement(ctx, req)
			require.NoError(t, err)
			require.NotNil(t, outcome)
			require.Equal(t, !tc.incomplete, outcome.Complete)
			require.Equal(t, result, outcome.Result)
			require.NoError(t, ctx.Err(), "inner failure must not rely on cancellation of the request")
			require.Equal(t, int32(2), calls.Load(), "one classification per scan")
		})
	}
}

func TestScanner_InferencePromptPolicyCompleteness(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                                          string
		verdict                                       *promptpolicy.Verdict
		err                                           error
		unavailable, disabled, incomplete, failClosed bool
	}{
		{name: "judge error", err: errors.New("judge unavailable"), incomplete: true},
		{name: "inner deadline", err: context.DeadlineExceeded, incomplete: true},
		{name: "nil verdict", incomplete: true},
		{name: "incomplete verdict", verdict: &promptpolicy.Verdict{}, incomplete: true},
		{name: "missing judge", unavailable: true, incomplete: true},
		{name: "disabled judge", disabled: true, incomplete: true},
		{name: "fail-closed judge error", err: errors.New("judge unavailable"), incomplete: true, failClosed: true},
		{name: "completed match", verdict: matchedJudgeVerdict(1, "unsafe operation")},
		{name: "completed clean", verdict: &promptpolicy.Verdict{Completed: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestRiskService(t)
			modelConfig := []byte(`{"fail_open":true}`)
			if tc.failClosed {
				modelConfig = []byte(`{"fail_open":false}`)
			}
			insertPromptBasedBlockPolicyWithConfig(t, ti, ctx, "inference prompt policy", "Block unsafe operations", modelConfig)
			judge := &fakePromptJudge{verdict: tc.verdict, err: tc.err}
			detector := promptpolicy.NewScanner(testenv.NewLogger(t), judge.Evaluate)
			if tc.unavailable {
				detector = nil
			}
			flags := promptPoliciesFlag(ctx)
			if tc.disabled {
				flags = &feature.InMemory{}
			}
			scanner, err := risk.NewScanner(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn, newTestCustomRuleAnalyzer(t, ti.conn), nil, nil, detector, flags, testCELEngine(t), metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()))
			require.NoError(t, err)
			auth, _ := contextvalues.GetAuthContext(ctx)
			req := realtimeScanRequest(auth.ActiveOrganizationID, *auth.ProjectID, auth.UserID, "read a file", message.ToolRequest, "read_file")
			result, err := scanner.ScanForEnforcement(ctx, req)
			require.NoError(t, err)
			if tc.failClosed || tc.name == "completed match" {
				require.NotNil(t, result)
			} else {
				require.Nil(t, result)
			}
			outcome, err := scanner.ScanForInferenceEnforcement(ctx, req)
			require.NoError(t, err)
			require.NotNil(t, outcome)
			require.Equal(t, !tc.incomplete, outcome.Complete)
			require.Equal(t, result, outcome.Result)
			require.NoError(t, ctx.Err())
			wantCalls := int32(2)
			if tc.disabled || tc.unavailable {
				wantCalls = 0
			}
			require.Equal(t, wantCalls, judge.calls.Load(), "one evaluation per scan when enabled")
		})
	}
}

type inferencePIIScanner struct {
	results []scanners.Result
	err     error
}

func (s *inferencePIIScanner) AnalyzeBatch(context.Context, []string, []string, float64, func()) ([]scanners.Result, error) {
	return s.results, s.err
}

func TestScanner_InferencePresidioCompleteness(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		detector   *inferencePIIScanner
		incomplete bool
	}{
		{name: "provider error", detector: &inferencePIIScanner{err: errors.New("analyzer unavailable")}, incomplete: true},
		{name: "inner deadline", detector: &inferencePIIScanner{err: context.DeadlineExceeded}, incomplete: true},
		{name: "missing result", detector: &inferencePIIScanner{}, incomplete: true},
		{name: "incomplete result", detector: &inferencePIIScanner{results: []scanners.Result{{}}}, incomplete: true},
		{name: "dead letter", detector: &inferencePIIScanner{results: []scanners.Result{{Completed: true, Findings: []scanners.Finding{{RuleID: "pii.dead_letter", DeadLetterReason: "unavailable"}}}}}, incomplete: true},
		{name: "completed clean", detector: &inferencePIIScanner{results: []scanners.Result{{Completed: true}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestRiskService(t)
			insertPresidioBlockPolicy(t, ti, ctx, "inference pii", []string{"EMAIL_ADDRESS"})
			scanner := newScannerWithDispatcher(t, ti, tc.detector, &feature.InMemory{}, nil)
			auth, _ := contextvalues.GetAuthContext(ctx)
			req := realtimeScanRequest(auth.ActiveOrganizationID, *auth.ProjectID, auth.UserID, "ordinary content", message.User, "")
			result, err := scanner.ScanForEnforcement(ctx, req)
			require.NoError(t, err)
			outcome, err := scanner.ScanForInferenceEnforcement(ctx, req)
			require.NoError(t, err)
			require.NotNil(t, outcome)
			require.Equal(t, !tc.incomplete, outcome.Complete)
			require.Equal(t, result, outcome.Result)
			require.NoError(t, ctx.Err())
		})
	}
}

func TestScanner_InferenceNoApplicablePoliciesComplete(t *testing.T) {
	t.Parallel()
	for _, outOfScope := range []bool{false, true} {
		t.Run(map[bool]string{false: "no policies", true: "out of scope"}[outOfScope], func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestRiskService(t)
			if outOfScope {
				config, err := risk_analysis.WithDetectionScopes(nil, []risk_analysis.DetectionScopeConfig{
					{Category: "prompt_injection", ScopeInclude: `kind == "tool_response"`, ScopeExempt: ""},
				})
				require.NoError(t, err)
				insertRealtimeBlockPolicy(t, ti, ctx, "tool-output injection only", []string{"prompt_injection"}, config)
			}
			scanner := newScannerWithDispatcher(t, ti, nil, &feature.InMemory{}, nil)
			auth, _ := contextvalues.GetAuthContext(ctx)
			outcome, err := scanner.ScanForInferenceEnforcement(ctx, realtimeScanRequest(auth.ActiveOrganizationID, *auth.ProjectID, auth.UserID, "ordinary user content", message.User, ""))
			require.NoError(t, err)
			require.NotNil(t, outcome)
			require.True(t, outcome.Complete)
			require.Nil(t, outcome.Result)
		})
	}
}

func TestScanner_InferenceCustomRuleFailurePreservesBuiltinDisposition(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"ordinary content", "export GITHUB_TOKEN=ghp_R2D2C3POLuk3Skywalker1234567890ab"} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestRiskService(t)
			auth, _ := contextvalues.GetAuthContext(ctx)
			_, err := riskrepo.New(ti.conn).CreateCustomDetectionRule(ctx, riskrepo.CreateCustomDetectionRuleParams{
				ProjectID:      *auth.ProjectID,
				OrganizationID: auth.ActiveOrganizationID,
				RuleID:         "custom.broken",
				Title:          "Broken rule",
				Description:    "Invalid expression tests suppressed evaluation failures",
				DetectionExpr:  pgtype.Text{String: "invalid CEL (", Valid: true},
				Severity:       "high",
			})
			require.NoError(t, err)
			policyID := uuid.New()
			_, err = riskrepo.New(ti.conn).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
				ID:             policyID,
				ProjectID:      *auth.ProjectID,
				OrganizationID: auth.ActiveOrganizationID,
				Name:           "broken custom rule with healthy builtin",
				Sources:        []string{"gitleaks"},
				CustomRuleIds:  []string{"custom.broken"},
				Enabled:        true,
				Action:         "block",
				AudienceType:   "everyone",
			})
			require.NoError(t, err)
			grantRiskPolicyToAllUsers(t, ti, ctx, auth.ActiveOrganizationID, policyID)
			scanner := newScannerWithDispatcher(t, ti, nil, &feature.InMemory{}, nil)
			req := realtimeScanRequest(auth.ActiveOrganizationID, *auth.ProjectID, auth.UserID, text, message.User, "")
			result, err := scanner.ScanForEnforcement(ctx, req)
			require.NoError(t, err)
			outcome, err := scanner.ScanForInferenceEnforcement(ctx, req)
			require.NoError(t, err)
			require.NotNil(t, outcome)
			require.False(t, outcome.Complete)
			require.Equal(t, result, outcome.Result)
			if text == "ordinary content" {
				require.Nil(t, outcome.Result)
			} else {
				require.NotNil(t, outcome.Result)
				require.Equal(t, "gitleaks", outcome.Result.Source)
			}
		})
	}
}
