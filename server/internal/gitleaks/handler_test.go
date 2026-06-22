package gitleaks_test

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/gitleaks"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// capturingPub records every Finding handed to Publish so tests can assert on
// the published payloads.
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

func newRequest(content string) *riskv1.GitleaksAnalysis {
	return riskv1.GitleaksAnalysis_builder{
		RequestId:         new("req-1"),
		ChatMessageId:     new("msg-1"),
		ProjectId:         new("proj-1"),
		OrganizationId:    new("org-1"),
		RiskPolicyId:      new("policy-1"),
		RiskPolicyVersion: new(int64(3)),
		CreatedAt:         new("2026-06-20T00:00:00Z"),
		Content:           &content,
	}.Build()
}

func TestHandle_PublishesGitleaksFinding(t *testing.T) {
	t.Parallel()

	pub, published := capturingPub(t)
	h, err := gitleaks.NewHandler(testenv.NewLogger(t), pub)
	require.NoError(t, err)

	content := `Here is my AWS key: AKIAIOSFODNN7REALKEY and secret: wJalrXUtnFEMI/K7MDENG/bPxRfiCYREALKEYXX`
	require.NoError(t, h.Handle(t.Context(), newRequest(content), gcp.MessageMetadata{}))

	require.NotEmpty(t, *published, "expected at least one finding published")

	var awsFinding *riskv1.Finding
	for _, f := range *published {
		// Request context propagates onto every finding.
		require.Equal(t, "gitleaks", f.GetSource())
		require.Equal(t, "req-1", f.GetRequestId())
		require.Equal(t, "msg-1", f.GetChatMessageId())
		require.Equal(t, int64(3), f.GetRiskPolicyVersion())
		require.NotEmpty(t, f.GetId())
		require.InDelta(t, 1.0, f.GetConfidence(), 0.0001)

		// Byte offsets must slice the matched secret out of the content.
		start, end := int(f.GetStartPos()), int(f.GetEndPos())
		require.GreaterOrEqual(t, start, 0)
		require.LessOrEqual(t, end, len(content))
		require.Equal(t, f.GetMatch(), content[start:end])

		if f.GetRuleId() == "secret.aws_access_token" {
			awsFinding = f
		}
	}
	require.NotNil(t, awsFinding, "expected an aws access token finding")
}

func TestHandle_CleanContentPublishesNothing(t *testing.T) {
	t.Parallel()

	pub, published := capturingPub(t)
	h, err := gitleaks.NewHandler(testenv.NewLogger(t), pub)
	require.NoError(t, err)

	require.NoError(t, h.Handle(t.Context(), newRequest("hello world, this is a normal message"), gcp.MessageMetadata{}))
	require.Empty(t, *published)
}
