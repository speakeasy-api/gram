package scanners_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
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

func (p *recordingFindingPublisher) Publish(_ context.Context, msg *riskv1.Finding, _ ...gcp.PublishOption) gcp.PublishResult {
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

func TestPublishFindingsDisambiguatesIdenticalFindings(t *testing.T) {
	t.Parallel()

	// Two findings with identical id inputs in one batch (e.g. two tool calls
	// in one message hitting the same rule with the same match and zero
	// positions) must publish under distinct ids, or ClickHouse uniqExact(id)
	// collapses distinct persisted findings. The suffixed ids must also be
	// stable across re-publishes so re-analysis still dedupes.
	finding := testFinding()
	meta := testFindingMetadata()
	firstPub := &recordingFindingPublisher{results: nil, messages: nil}
	secondPub := &recordingFindingPublisher{results: nil, messages: nil}

	batch := []scanners.Finding{finding, finding, finding}
	firstPublished, _, firstErr := scanners.PublishFindings(t.Context(), testenv.NewLogger(t), firstPub, meta, batch, "cli destructive")
	secondPublished, _, secondErr := scanners.PublishFindings(t.Context(), testenv.NewLogger(t), secondPub, meta, batch, "cli destructive")

	require.NoError(t, firstErr)
	require.NoError(t, secondErr)
	require.Equal(t, 3, firstPublished)
	require.Equal(t, 3, secondPublished)
	require.Len(t, firstPub.messages, 3)

	ids := map[string]struct{}{}
	for i, msg := range firstPub.messages {
		ids[msg.GetId()] = struct{}{}
		require.Equal(t, msg.GetId(), secondPub.messages[i].GetId(), "ids must be stable across re-publishes")
	}
	require.Len(t, ids, 3, "identical findings in one batch must get distinct ids")
}

func TestPublishFindingsMessageAnchorKeepsHistoricalDeterministicID(t *testing.T) {
	t.Parallel()

	finding := testFinding()
	meta := testFindingMetadata()
	pub := &recordingFindingPublisher{results: nil, messages: nil}

	published, _, err := scanners.PublishFindings(t.Context(), testenv.NewLogger(t), pub, meta, []scanners.Finding{finding}, "prompt injection")

	require.NoError(t, err)
	require.Equal(t, 1, published)
	require.Len(t, pub.messages, 1)
	expectedParts := []string{
		meta.RequestID,
		meta.ChatMessageID,
		meta.ProjectID,
		meta.OrganizationID,
		meta.RiskPolicyID,
		strconv.FormatInt(meta.RiskPolicyVersion, 10),
		finding.Source,
		finding.RuleID,
		strconv.Itoa(finding.StartPos),
		strconv.Itoa(finding.EndPos),
		finding.Match,
	}
	expected := uuid.NewSHA1(uuid.NameSpaceURL, []byte("gram:risk:finding:"+strings.Join(expectedParts, "\x00")))
	require.Equal(t, expected.String(), pub.messages[0].GetId())
}

func TestPublishFindingsPublishesContentPartAnchor(t *testing.T) {
	t.Parallel()

	meta := testFindingMetadata()
	meta.ChatMessageID = ""
	meta.ContentPartID = "part-1"
	pub := &recordingFindingPublisher{results: nil, messages: nil}

	published, _, err := scanners.PublishFindings(t.Context(), testenv.NewLogger(t), pub, meta, []scanners.Finding{testFinding()}, "prompt policy")

	require.NoError(t, err)
	require.Equal(t, 1, published)
	require.Len(t, pub.messages, 1)
	require.Empty(t, pub.messages[0].GetChatMessageId())
	require.Equal(t, "part-1", pub.messages[0].GetContentPartId())
}

// The published Finding carries the reveal metadata: field/path pass through
// from the domain finding, surface derives from (source, field, path) via
// FindingSurface, and tool_call_id is stamped only from McpLookupToolCallID —
// the real recorded call id shadow_mcp sets. Custom-rule span findings leave
// it unset (their spans only know the tool NAME, which is not an id).
func TestStartPublishFindingsStampsRevealMetadata(t *testing.T) {
	t.Parallel()

	spanFinding := testFinding()
	spanFinding.Source = "custom"
	spanFinding.Field = "tool.args"
	spanFinding.Path = "command.0"

	contentFinding := testFinding()
	contentFinding.Source = "prompt_injection"

	judgeFinding := testFinding()
	judgeFinding.Source = "llm_judge"

	derivedFinding := testFinding()
	derivedFinding.Source = "shadow_mcp"
	derivedFinding.McpLookupToolCallID = "call_2"

	pub := &recordingFindingPublisher{results: nil, messages: nil}
	results, _ := scanners.StartPublishFindings(t.Context(), pub, testFindingMetadata(), []scanners.Finding{spanFinding, contentFinding, judgeFinding, derivedFinding})
	require.Len(t, results, 4)
	require.Len(t, pub.messages, 4)

	span := pub.messages[0]
	require.Equal(t, scanners.SurfaceJSONPath, span.GetSurface())
	require.Equal(t, "tool.args", span.GetField())
	require.Equal(t, "command.0", span.GetPath())
	require.Empty(t, span.GetToolCallId())

	require.Equal(t, scanners.SurfaceContent, pub.messages[1].GetSurface())
	require.Empty(t, pub.messages[1].GetField())
	require.Empty(t, pub.messages[1].GetToolCallId())

	require.Equal(t, scanners.SurfaceNone, pub.messages[2].GetSurface())

	require.Equal(t, scanners.SurfaceDerived, pub.messages[3].GetSurface())
	require.Equal(t, "call_2", pub.messages[3].GetToolCallId())
}

// The span-level arms must stay byte-identical with the offline backfill's
// spanSurface (server/cmd/tools/migrations/riskfindings); the field=="" arms
// are the live per-source defaults.
func TestFindingSurface(t *testing.T) {
	t.Parallel()

	cases := []struct {
		source, field, path string
		want                string
	}{
		{source: "custom", field: "tool.args", path: "command.0", want: "json_path"},
		{source: "custom", field: "content", path: "cmd", want: "json_path"},
		{source: "custom", field: "tool.args", path: "", want: "tool_args"},
		{source: "custom", field: "content", path: "", want: "content"},
		{source: "custom", field: "prompt", path: "", want: "content"},
		{source: "custom", field: "assistant", path: "", want: "content"},
		{source: "custom", field: "tool_result", path: "", want: "content"},
		{source: "custom", field: "tool.server", path: "", want: "derived"},
		{source: "custom", field: "tool.function", path: "", want: "derived"},
		{source: "custom", field: "tool.name", path: "", want: ""},
		{source: "gitleaks", field: "", path: "", want: "content"},
		{source: "presidio", field: "", path: "", want: "content"},
		{source: "prompt_injection", field: "", path: "", want: "content"},
		{source: "llm_judge", field: "", path: "", want: "none"},
		{source: "shadow_mcp", field: "", path: "", want: "derived"},
		{source: "account_identity", field: "", path: "", want: "derived"},
		{source: "destructive_tool", field: "", path: "", want: "derived"},
		{source: "cli_destructive", field: "", path: "", want: "derived"},
		{source: "custom", field: "", path: "", want: ""},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, scanners.FindingSurface(tc.source, tc.field, tc.path),
			"FindingSurface(%q, %q, %q)", tc.source, tc.field, tc.path)
	}
}

func testFindingMetadata() scanners.FindingMetadata {
	return scanners.FindingMetadata{
		RequestID:         "req-1",
		ChatMessageID:     "msg-1",
		ContentPartID:     "",
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
