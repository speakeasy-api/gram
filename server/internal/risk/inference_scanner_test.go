package risk_test

import (
	"context"
	"errors"
	"testing"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/risk"
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
		name      string
		verdict   promptinjection.Result
		err       error
		wantError bool
	}{
		{name: "provider error", err: errors.New("classifier unavailable"), wantError: true},
		{name: "inner deadline", err: context.DeadlineExceeded, wantError: true},
		{name: "unavailable verdict", verdict: promptinjection.Result{Label: promptinjection.LabelUnavailable}, wantError: true},
		{name: "incomplete verdict", verdict: promptinjection.Result{Label: promptinjection.LabelSafe}, wantError: true},
		{name: "completed clean", verdict: promptinjection.Result{Label: promptinjection.LabelSafe, Completed: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestRiskService(t)
			insertRealtimeBlockPolicy(t, ti, ctx, "inference injection", []string{"prompt_injection"}, nil)
			classifier := promptinjection.Classifier(func(context.Context, promptinjection.Request) ([]promptinjection.Result, error) {
				return []promptinjection.Result{tc.verdict}, tc.err
			})
			scanner, err := risk.NewScanner(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn, newTestCustomRuleAnalyzer(t, ti.conn), nil, promptinjection.NewScanner(testenv.NewLogger(t), classifier), nil, &feature.InMemory{}, testCELEngine(t), metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()))
			require.NoError(t, err)
			auth, _ := contextvalues.GetAuthContext(ctx)
			req := realtimeScanRequest(auth.ActiveOrganizationID, *auth.ProjectID, auth.UserID, "untrusted tool output", message.ToolResponse, "read_file")
			result, err := scanner.ScanForEnforcement(ctx, req)
			require.NoError(t, err)
			require.Nil(t, result)
			result, err = scanner.ScanForInferenceEnforcement(ctx, req)
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Nil(t, result)
			require.NoError(t, ctx.Err(), "inner failure must not rely on cancellation of the request")
		})
	}
}

func TestScanner_InferencePromptPolicyCompleteness(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                             string
		verdict                          *promptpolicy.Verdict
		err                              error
		unavailable, disabled, wantError bool
	}{
		{name: "judge error", err: errors.New("judge unavailable"), wantError: true},
		{name: "inner deadline", err: context.DeadlineExceeded, wantError: true},
		{name: "nil verdict", wantError: true},
		{name: "incomplete verdict", verdict: &promptpolicy.Verdict{}, wantError: true},
		{name: "missing judge", unavailable: true, wantError: true},
		{name: "disabled judge", disabled: true, wantError: true},
		{name: "completed clean", verdict: &promptpolicy.Verdict{Completed: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestRiskService(t)
			insertPromptBasedBlockPolicyWithConfig(t, ti, ctx, "inference prompt policy", "Block unsafe operations", []byte(`{"fail_open":true}`))
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
			require.Nil(t, result)
			result, err = scanner.ScanForInferenceEnforcement(ctx, req)
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Nil(t, result)
			require.NoError(t, ctx.Err())
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
		name      string
		detector  *inferencePIIScanner
		wantError bool
	}{
		{name: "provider error", detector: &inferencePIIScanner{err: errors.New("analyzer unavailable")}, wantError: true},
		{name: "inner deadline", detector: &inferencePIIScanner{err: context.DeadlineExceeded}, wantError: true},
		{name: "missing result", detector: &inferencePIIScanner{}, wantError: true},
		{name: "incomplete result", detector: &inferencePIIScanner{results: []scanners.Result{{}}}, wantError: true},
		{name: "dead letter", detector: &inferencePIIScanner{results: []scanners.Result{{Completed: true, Findings: []scanners.Finding{{RuleID: "pii.dead_letter", DeadLetterReason: "unavailable"}}}}}, wantError: true},
		{name: "completed clean", detector: &inferencePIIScanner{results: []scanners.Result{{Completed: true}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestRiskService(t)
			insertPresidioBlockPolicy(t, ti, ctx, "inference pii", []string{"EMAIL_ADDRESS"})
			scanner := newScannerWithDispatcher(t, ti, tc.detector, &feature.InMemory{}, nil)
			auth, _ := contextvalues.GetAuthContext(ctx)
			req := realtimeScanRequest(auth.ActiveOrganizationID, *auth.ProjectID, auth.UserID, "ordinary content", message.User, "")
			_, err := scanner.ScanForEnforcement(ctx, req)
			require.NoError(t, err)
			result, err := scanner.ScanForInferenceEnforcement(ctx, req)
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Nil(t, result)
			require.NoError(t, ctx.Err())
		})
	}
}
