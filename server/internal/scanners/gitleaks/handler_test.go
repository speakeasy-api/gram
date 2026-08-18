package gitleaks_test

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/scanners/gitleaks"
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
	h := gitleaks.NewHandler(testenv.NewLogger(t), pub)

	// The access key id anchors detection but is not itself reported (it is an
	// identifier, not a secret); the secret access key is the reported finding.
	content := `AccessKeyId: ` + fakeAccessKeyID + `, SecretAccessKey: ` + fakeSecret
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

		if f.GetRuleId() == "secret.aws_secret_access_key" {
			awsFinding = f
		}
		require.NotEqual(t, "secret.aws_access_token", f.GetRuleId(),
			"the access key id must not be reported as a finding")
	}
	require.NotNil(t, awsFinding, "expected an aws secret access key finding")
}

func TestHandle_PublishesGitleaksFindingForContentPart(t *testing.T) {
	t.Parallel()

	pub, published := capturingPub(t)
	h := gitleaks.NewHandler(testenv.NewLogger(t), pub)

	content := `AccessKeyId: ` + fakeAccessKeyID + `, SecretAccessKey: ` + fakeSecret
	req := newRequest(content)
	req.ClearChatMessageId()
	req.SetContentPartId("part-1")
	require.NoError(t, h.Handle(t.Context(), req, gcp.MessageMetadata{}))

	require.NotEmpty(t, *published, "expected at least one finding published")
	for _, f := range *published {
		require.Empty(t, f.GetChatMessageId())
		require.Equal(t, "part-1", f.GetContentPartId())
	}
}

func TestHandle_CleanContentPublishesNothing(t *testing.T) {
	t.Parallel()

	pub, published := capturingPub(t)
	h := gitleaks.NewHandler(testenv.NewLogger(t), pub)

	require.NoError(t, h.Handle(t.Context(), newRequest("hello world, this is a normal message"), gcp.MessageMetadata{}))
	require.Empty(t, *published)
}

// The stream handler scans the verbatim request content, so every published
// finding carries surface "content" and no span attribution.
func TestHandle_StampsContentSurface(t *testing.T) {
	t.Parallel()

	pub, published := capturingPub(t)
	h := gitleaks.NewHandler(testenv.NewLogger(t), pub)

	content := `SecretAccessKey: ` + fakeSecret
	require.NoError(t, h.Handle(t.Context(), newRequest(content), gcp.MessageMetadata{}))

	require.NotEmpty(t, *published)
	for _, f := range *published {
		require.Equal(t, "content", f.GetSurface())
		require.Empty(t, f.GetField())
		require.Empty(t, f.GetPath())
		require.Empty(t, f.GetToolCallId())
	}
}

// Redelivering the same scan request republishes every finding under the same
// deterministic id, so ClickHouse's id-level dedup collapses the duplicates
// instead of counting them twice (the old uuid.NewV7-per-publish minted a new
// row per redelivery).
func TestHandle_RedeliveryKeepsDeterministicIDs(t *testing.T) {
	t.Parallel()

	firstPub, firstPublished := capturingPub(t)
	secondPub, secondPublished := capturingPub(t)

	content := `AccessKeyId: ` + fakeAccessKeyID + `, SecretAccessKey: ` + fakeSecret
	require.NoError(t, gitleaks.NewHandler(testenv.NewLogger(t), firstPub).Handle(t.Context(), newRequest(content), gcp.MessageMetadata{}))
	require.NoError(t, gitleaks.NewHandler(testenv.NewLogger(t), secondPub).Handle(t.Context(), newRequest(content), gcp.MessageMetadata{}))

	require.NotEmpty(t, *firstPublished)
	require.Len(t, *secondPublished, len(*firstPublished))
	for i, f := range *firstPublished {
		require.NotEmpty(t, f.GetId())
		require.Equal(t, f.GetId(), (*secondPublished)[i].GetId(), "ids must be stable across redeliveries")
	}
}
