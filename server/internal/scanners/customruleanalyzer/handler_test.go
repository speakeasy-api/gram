package customruleanalyzer_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners/customruleanalyzer"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newTestScanner(t *testing.T, conn repo.DBTX) *customruleanalyzer.Scanner {
	t.Helper()
	s, err := customruleanalyzer.NewScanner(conn)
	require.NoError(t, err)

	return s
}

func TestHandle_PublishesCustomRuleFinding(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	p := seedProject(t, conn)
	seedCustomRule(t, conn, p, "custom.secret", `content.matchRegex("secret")`)

	pub, published := capturingPub(t)
	scanner := newTestScanner(t, conn)
	h := customruleanalyzer.NewHandler(testenv.NewLogger(t), scanner, pub)

	req := newRequest(p, "here is a secret value", "custom.secret")
	require.NoError(t, h.Handle(t.Context(), req, gcp.MessageMetadata{}))

	require.Len(t, *published, 1)
	f := (*published)[0]
	require.Equal(t, "custom", f.GetSource())
	require.Equal(t, "custom.secret", f.GetRuleId())
	require.Equal(t, "req-1", f.GetRequestId())
	require.Equal(t, "msg-1", f.GetChatMessageId())
	require.Equal(t, p.projectID.String(), f.GetProjectId())
	require.Equal(t, p.orgID, f.GetOrganizationId())
	require.Equal(t, "policy-1", f.GetRiskPolicyId())
	require.Equal(t, int64(3), f.GetRiskPolicyVersion())
	require.Equal(t, "test rule description", f.GetDescription())
	require.NotEmpty(t, f.GetId())
	require.InDelta(t, 1.0, f.GetConfidence(), 0.0001)

	// Byte offsets must slice the matched value out of the content.
	start, end := int(f.GetStartPos()), int(f.GetEndPos())
	require.Equal(t, "secret", f.GetMatch())
	require.Equal(t, req.GetContent()[start:end], f.GetMatch())
}

// A content-rule finding carries its span attribution as reveal metadata:
// field "content", surface "content", and no tool call anchor.
func TestHandle_StampsContentSpanMetadata(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	p := seedProject(t, conn)
	seedCustomRule(t, conn, p, "custom.secret", `content.matchRegex("secret")`)

	pub, published := capturingPub(t)
	h := customruleanalyzer.NewHandler(testenv.NewLogger(t), newTestScanner(t, conn), pub)

	require.NoError(t, h.Handle(t.Context(), newRequest(p, "here is a secret value", "custom.secret"), gcp.MessageMetadata{}))

	require.Len(t, *published, 1)
	f := (*published)[0]
	require.Equal(t, "content", f.GetField())
	require.Equal(t, "content", f.GetSurface())
	require.Empty(t, f.GetPath())
	require.Empty(t, f.GetToolCallId())
}

// A tool-args rule stamps the span's field, gjson path, and json_path surface
// onto every published finding — and never a tool_call_id: the span only
// carries the tool NAME, which must not masquerade as a recorded call id.
func TestHandle_StampsToolCallSpanMetadata(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	p := seedProject(t, conn)
	// filter (not exists) visits every tool call, so both offending calls span.
	seedCustomRule(t, conn, p, "custom.drop", `tool_calls.filter(t, t.args.get("command").matchRegex("DROP TABLE")).size() > 0`)

	pub, published := capturingPub(t)
	h := customruleanalyzer.NewHandler(testenv.NewLogger(t), newTestScanner(t, conn), pub)

	req := newRequest(p, "", "custom.drop")
	req.SetKind("tool_request")
	req.SetToolCalls([]*riskv1.CustomRulesAnalysis_ToolCall{
		riskv1.CustomRulesAnalysis_ToolCall_builder{
			Name:      new("db:exec"),
			Arguments: new(`{"command":"DROP TABLE users"}`),
		}.Build(),
		riskv1.CustomRulesAnalysis_ToolCall_builder{
			Name:      new("db:exec"),
			Arguments: new(`{"command":"DROP TABLE sessions"}`),
		}.Build(),
	})
	require.NoError(t, h.Handle(t.Context(), req, gcp.MessageMetadata{}))

	require.Len(t, *published, 2)
	for _, f := range *published {
		require.Equal(t, "tool.args", f.GetField())
		require.Equal(t, "command", f.GetPath())
		require.Equal(t, "json_path", f.GetSurface())
		require.Empty(t, f.GetToolCallId(), "custom findings must never claim a call id")
	}
}

// Redelivering the same scan request republishes every finding under the same
// deterministic id, so ClickHouse's id-level dedup collapses the duplicates
// instead of counting them twice (the old uuid.NewV7-per-publish minted a new
// row per redelivery).
func TestHandle_RedeliveryKeepsDeterministicIDs(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	p := seedProject(t, conn)
	seedCustomRule(t, conn, p, "custom.secret", `content.matchRegex("secret")`)

	firstPub, firstPublished := capturingPub(t)
	secondPub, secondPublished := capturingPub(t)
	scanner := newTestScanner(t, conn)

	require.NoError(t, customruleanalyzer.NewHandler(testenv.NewLogger(t), scanner, firstPub).Handle(t.Context(), newRequest(p, "a secret and another secret", "custom.secret"), gcp.MessageMetadata{}))
	require.NoError(t, customruleanalyzer.NewHandler(testenv.NewLogger(t), scanner, secondPub).Handle(t.Context(), newRequest(p, "a secret and another secret", "custom.secret"), gcp.MessageMetadata{}))

	require.NotEmpty(t, *firstPublished)
	require.Len(t, *secondPublished, len(*firstPublished))
	for i, f := range *firstPublished {
		require.NotEmpty(t, f.GetId())
		require.Equal(t, f.GetId(), (*secondPublished)[i].GetId(), "ids must be stable across redeliveries")
	}
}

// Only rules named in the request's custom_rule_ids are evaluated, mirroring the
// policy-scoped selection of the in-process scan.
func TestHandle_UnselectedRuleNotApplied(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	p := seedProject(t, conn)
	seedCustomRule(t, conn, p, "custom.secret", `content.matchRegex("secret")`)

	scanner := newTestScanner(t, conn)
	pub, published := capturingPub(t)
	h := customruleanalyzer.NewHandler(testenv.NewLogger(t), scanner, pub)

	// Content matches the rule, but the selected id does not, so nothing fires.
	req := newRequest(p, "here is a secret value", "custom.other")
	require.NoError(t, h.Handle(t.Context(), req, gcp.MessageMetadata{}))
	require.Empty(t, *published)
}

// A malformed project id fails the uuid parse in Handle before any scan or
// publish happens.
func TestHandle_InvalidProjectID(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	p := seedProject(t, conn)

	scanner := newTestScanner(t, conn)
	pub, published := capturingPub(t)
	h := customruleanalyzer.NewHandler(testenv.NewLogger(t), scanner, pub)

	req := newRequest(p, "here is a secret value", "custom.secret")
	req.SetProjectId("not-a-uuid")

	err := h.Handle(t.Context(), req, gcp.MessageMetadata{})
	require.ErrorContains(t, err, "invalid project id")
	require.Empty(t, *published)
}

// A syntactically invalid CEL rule stored in the database is a permanent error:
// Handle logs and swallows it (returns nil) rather than nacking, so the bad rule
// cannot poison the subscription by redelivering the message forever.
func TestHandle_InvalidRuleSwallowed(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	p := seedProject(t, conn)
	seedCustomRule(t, conn, p, "custom.broken", `this is not valid cel !!!`)

	scanner := newTestScanner(t, conn)
	pub, published := capturingPub(t)
	h := customruleanalyzer.NewHandler(testenv.NewLogger(t), scanner, pub)

	req := newRequest(p, "here is a secret value", "custom.broken")
	require.NoError(t, h.Handle(t.Context(), req, gcp.MessageMetadata{}))
	require.Empty(t, *published)
}

func TestHandle_CleanContentPublishesNothing(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	p := seedProject(t, conn)
	seedCustomRule(t, conn, p, "custom.secret", `content.matchRegex("secret")`)

	scanner := newTestScanner(t, conn)
	pub, published := capturingPub(t)
	h := customruleanalyzer.NewHandler(testenv.NewLogger(t), scanner, pub)

	req := newRequest(p, "totally benign message", "custom.secret")
	require.NoError(t, h.Handle(t.Context(), req, gcp.MessageMetadata{}))
	require.Empty(t, *published)
}
