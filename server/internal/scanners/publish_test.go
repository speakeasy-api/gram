package scanners_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type recordingFindingPublisher struct {
	results  []gcp.PublishResult
	messages []*riskv1.Finding
}

func (p *recordingFindingPublisher) Publish(_ context.Context, msg *riskv1.Finding) gcp.PublishResult {
	p.messages = append(p.messages, msg)
	if len(p.results) == 0 {
		return gcp.NewSuccessPublishResult()
	}
	result := p.results[0]
	p.results = p.results[1:]
	return result
}

func (p *recordingFindingPublisher) Stop(context.Context) error {
	return nil
}

type failedPublishResult struct {
	err error
}

func (r failedPublishResult) Ready() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (r failedPublishResult) Get(context.Context) (string, error) {
	return "", r.err
}

func TestPublishFindingsReturnsPublishErrors(t *testing.T) {
	t.Parallel()

	pub := &recordingFindingPublisher{
		results: []gcp.PublishResult{
			failedPublishResult{err: errors.New("pubsub unavailable")},
		},
		messages: nil,
	}
	published, ruleIDs, err := scanners.PublishFindings(t.Context(), testenv.NewLogger(t), pub, testFindingMetadata(), []scanners.Finding{testFinding()}, "prompt policy")

	require.ErrorContains(t, err, "publish prompt policy findings")
	require.Equal(t, 0, published)
	require.Equal(t, []string{"prompt-policy"}, ruleIDs)
	require.Len(t, pub.messages, 1)
}

func TestPublishFindingsUsesDeterministicIDs(t *testing.T) {
	t.Parallel()

	finding := testFinding()
	meta := testFindingMetadata()
	firstPub := &recordingFindingPublisher{results: nil, messages: nil}
	secondPub := &recordingFindingPublisher{results: nil, messages: nil}

	firstPublished, _, firstErr := scanners.PublishFindings(t.Context(), testenv.NewLogger(t), firstPub, meta, []scanners.Finding{finding}, "prompt injection")
	secondPublished, _, secondErr := scanners.PublishFindings(t.Context(), testenv.NewLogger(t), secondPub, meta, []scanners.Finding{finding}, "prompt injection")

	require.NoError(t, firstErr)
	require.NoError(t, secondErr)
	require.Equal(t, 1, firstPublished)
	require.Equal(t, 1, secondPublished)
	require.Len(t, firstPub.messages, 1)
	require.Len(t, secondPub.messages, 1)
	require.Equal(t, firstPub.messages[0].GetId(), secondPub.messages[0].GetId())
}

func testFindingMetadata() scanners.FindingMetadata {
	return scanners.FindingMetadata{
		RequestID:         "req-1",
		ChatMessageID:     "msg-1",
		ProjectID:         "project-1",
		OrganizationID:    "org-1",
		RiskPolicyID:      "policy-1",
		RiskPolicyVersion: 3,
	}
}

func testFinding() scanners.Finding {
	return scanners.Finding{
		RuleID:              "prompt-policy",
		Description:         "matched prompt policy",
		Match:               "delete production",
		StartPos:            0,
		EndPos:              17,
		Tags:                []string{"prompt"},
		Source:              "prompt_policy",
		Confidence:          0.9,
		DeadLetterReason:    "",
		McpLookupToolCallID: "",
		SpanGroupKey:        "",
		Field:               "",
		Path:                "",
	}
}
