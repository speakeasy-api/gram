package customruleanalyzer_test

import (
	"testing"

	"github.com/stretchr/testify/require"

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
