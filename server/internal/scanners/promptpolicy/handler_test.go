package promptpolicy_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func capturingPub(t *testing.T) (*gcp.MockPublisher[*riskv1.Finding], *[]*riskv1.Finding) {
	t.Helper()
	pub := gcp.NewMockPublisher[*riskv1.Finding]()
	var published []*riskv1.Finding
	pub.On("Publish", mock.Anything, mock.Anything).
		Return(gcp.NewSuccessPublishResult()).
		Run(func(args mock.Arguments) {
			f, ok := args.Get(1).(*riskv1.Finding)
			require.True(t, ok)
			published = append(published, f)
		})
	return pub, &published
}

func newRequest(content string) *riskv1.PromptPolicyAnalysis {
	modelConfig := []byte(`{"model":"openai/gpt-4.1-mini","fail_open":false}`)
	return riskv1.PromptPolicyAnalysis_builder{
		RequestId:         new("req-1"),
		ChatMessageId:     new("msg-1"),
		ProjectId:         new("proj-1"),
		OrganizationId:    new("org-1"),
		RiskPolicyId:      new("policy-1"),
		RiskPolicyVersion: new(int64(3)),
		CreatedAt:         new("2026-06-20T00:00:00Z"),
		Content:           &content,
		UserId:            new("user-1"),
		Prompt:            new("block unsafe requests"),
		ModelConfig:       modelConfig,
		MessageType:       new("user_message"),
		Body:              &content,
		ToolName:          new(""),
	}.Build()
}

func TestHandle_PublishesPromptPolicyFinding(t *testing.T) {
	t.Parallel()

	pub, published := capturingPub(t)
	evaluator := func(_ context.Context, in promptpolicy.Input) (*promptpolicy.Verdict, error) {
		require.Equal(t, "org-1", in.OrgID)
		require.Equal(t, "proj-1", in.ProjectID)
		require.Equal(t, "user-1", in.UserID)
		require.Equal(t, "block unsafe requests", in.Prompt)
		require.Equal(t, "delete production", in.Message.Body)
		require.Equal(t, "openai/gpt-4.1-mini", in.Config.Model)
		require.False(t, in.Config.FailOpen)
		return &promptpolicy.Verdict{
			Matched:          true,
			Confidence:       0.88,
			Rationale:        "Message matched the prompt-based policy.",
			CostUSD:          0,
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
		}, nil
	}
	scanner := promptpolicy.NewScanner(testenv.NewLogger(t), evaluator)
	h := promptpolicy.NewHandler(testenv.NewLogger(t), scanner, pub)

	require.NoError(t, h.Handle(t.Context(), newRequest("delete production"), gcp.MessageMetadata{}))

	require.Len(t, *published, 1)
	f := (*published)[0]
	require.Equal(t, promptpolicy.Source, f.GetSource())
	require.Equal(t, promptpolicy.Rule, f.GetRuleId())
	require.Equal(t, "req-1", f.GetRequestId())
	require.Equal(t, "msg-1", f.GetChatMessageId())
	require.Equal(t, int64(3), f.GetRiskPolicyVersion())
	require.NotEmpty(t, f.GetId())
	require.InDelta(t, 0.88, f.GetConfidence(), 0.0001)
}

func TestHandle_CleanPromptPolicyContentPublishesNothing(t *testing.T) {
	t.Parallel()

	pub, published := capturingPub(t)
	evaluator := func(context.Context, promptpolicy.Input) (*promptpolicy.Verdict, error) {
		return &promptpolicy.Verdict{
			Matched:          false,
			Confidence:       0,
			Rationale:        "",
			CostUSD:          0,
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
		}, nil
	}
	scanner := promptpolicy.NewScanner(testenv.NewLogger(t), evaluator)
	h := promptpolicy.NewHandler(testenv.NewLogger(t), scanner, pub)

	require.NoError(t, h.Handle(t.Context(), newRequest("hello world"), gcp.MessageMetadata{}))
	require.Empty(t, *published)
}
