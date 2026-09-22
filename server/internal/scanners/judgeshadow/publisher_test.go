package judgeshadow

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
)

func TestEnabledRequiresFlagsAndOrgID(t *testing.T) {
	t.Parallel()

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagJevPromptInjectionShadow, "org-1", true)

	require.False(t, Enabled(t.Context(), nil, promptinjection.Source, "org-1"), "nil provider must disable shadow work")
	require.False(t, Enabled(t.Context(), flags, promptinjection.Source, ""), "empty org ID must disable shadow work")
	require.False(t, Enabled(t.Context(), flags, promptinjection.Source, "org-2"), "unflagged org must stay disabled")
	require.True(t, Enabled(t.Context(), flags, promptinjection.Source, "org-1"))
	require.False(t, Enabled(t.Context(), flags, promptpolicy.Source, "org-1"), "flags are per-detector")
	require.False(t, Enabled(t.Context(), flags, "unknown_detector", "org-1"))
}

func TestWrapPolicyReturnsBaselineUnchanged(t *testing.T) {
	t.Parallel()

	flags := &feature.InMemory{}
	pub := gcp.NewMockPublisher[*riskv1.JudgeShadowAnalysis]()
	p := NewPublisher(slog.Default(), flags, pub, 0)

	baselineErr := errors.New("judge degraded")
	baseline := func(_ context.Context, _ promptpolicy.Input) (*promptpolicy.Verdict, error) {
		return &promptpolicy.Verdict{Matched: true, Completed: true}, baselineErr
	}

	verdict, err := p.WrapPolicy(baseline)(t.Context(), promptpolicy.Input{OrgID: "org-1", ProjectID: "proj-1"})

	require.ErrorIs(t, err, baselineErr)
	require.NotNil(t, verdict)
	require.True(t, verdict.Matched)
	pub.AssertNotCalled(t, "Publish")
}

func TestWrapInjectionReturnsBaselineUnchanged(t *testing.T) {
	t.Parallel()

	flags := &feature.InMemory{}
	pub := gcp.NewMockPublisher[*riskv1.JudgeShadowAnalysis]()
	p := NewPublisher(slog.Default(), flags, pub, 0)

	want := []promptinjection.Result{{Label: promptinjection.LabelInjection, Completed: true}}
	baseline := func(_ context.Context, _ promptinjection.Request) ([]promptinjection.Result, error) {
		return want, nil
	}

	req := promptinjection.Request{
		OrgID:     "org-1",
		ProjectID: "proj-1",
		Messages:  []judgemessage.Message{judgemessage.New(message.ToolRequest, "Bash", "{}")},
	}
	got, err := p.WrapInjection(baseline)(t.Context(), req)

	require.NoError(t, err)
	require.Equal(t, want, got)
	pub.AssertNotCalled(t, "Publish")
}

func TestWrapPolicyPublishesWhenSampledAndEnabled(t *testing.T) {
	t.Parallel()

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagJevPromptPolicyShadow, "org-1", true)
	pub := gcp.NewMockPublisher[*riskv1.JudgeShadowAnalysis]()
	pub.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewSuccessPublishResult())

	p := NewPublisher(slog.Default(), flags, pub, 1)

	baseline := func(_ context.Context, _ promptpolicy.Input) (*promptpolicy.Verdict, error) {
		return &promptpolicy.Verdict{Matched: true, Completed: true, Model: "baseline-model"}, nil
	}

	_, err := p.WrapPolicy(baseline)(t.Context(), promptpolicy.Input{OrgID: "org-1", ProjectID: "proj-1", Prompt: "no secrets"})

	require.NoError(t, err)
	pub.AssertNumberOfCalls(t, "Publish", 1)
	event := pub.Calls[0].Arguments.Get(1).(*riskv1.JudgeShadowAnalysis)
	require.Equal(t, promptpolicy.Source, event.GetDetector())
	require.Equal(t, "org-1", event.GetOrganizationId())
	require.Equal(t, "match", event.GetBaselineOutcome())
	require.Equal(t, "baseline-model", event.GetBaselineModel())
}
