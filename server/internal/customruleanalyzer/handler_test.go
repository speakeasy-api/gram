package customruleanalyzer_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/customruleanalyzer"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestHandle_PublishesCustomRuleFinding(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	p := seedProject(t, conn)
	seedCustomRule(t, conn, p, "custom.secret", `content.matchRegex("secret")`)

	pub, published := capturingPub(t)
	h, err := customruleanalyzer.NewHandler(testenv.NewLogger(t), conn, pub)
	require.NoError(t, err)

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

	pub, published := capturingPub(t)
	h, err := customruleanalyzer.NewHandler(testenv.NewLogger(t), conn, pub)
	require.NoError(t, err)

	// Content matches the rule, but the selected id does not, so nothing fires.
	req := newRequest(p, "here is a secret value", "custom.other")
	require.NoError(t, h.Handle(t.Context(), req, gcp.MessageMetadata{}))
	require.Empty(t, *published)
}

func TestHandle_CleanContentPublishesNothing(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	p := seedProject(t, conn)
	seedCustomRule(t, conn, p, "custom.secret", `content.matchRegex("secret")`)

	pub, published := capturingPub(t)
	h, err := customruleanalyzer.NewHandler(testenv.NewLogger(t), conn, pub)
	require.NoError(t, err)

	req := newRequest(p, "totally benign message", "custom.secret")
	require.NoError(t, h.Handle(t.Context(), req, gcp.MessageMetadata{}))
	require.Empty(t, *published)
}
