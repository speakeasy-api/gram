package chat_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/chat"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestLoadChatOverview_ExactResourceGrantWithoutTranscriptAudit(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	parent := seedChat(t, ctx, ti, "another-user", "", "parent session")
	other := seedChat(t, ctx, ti, "another-user", "", "other session")
	messages := seedNMessages(t, ctx, ti, parent, 3)
	attachRiskTo(t, ctx, ti, messages[0], messages[2])
	scopedCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeChatRead, parent.String()))
	before, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionChatSessionAccess)
	require.NoError(t, err)
	overview, err := ti.service.LoadChatOverview(scopedCtx, &gen.LoadChatOverviewPayload{ID: parent.String()})
	require.NoError(t, err)
	require.Equal(t, parent.String(), overview.ID)
	require.Equal(t, 3, overview.NumMessages)
	require.NotNil(t, overview.RiskFindingsCount)
	require.Equal(t, 2, *overview.RiskFindingsCount)
	err = repo.New(ti.conn).DisableRiskPoliciesForTest(ctx, ti.projectID)
	require.NoError(t, err)
	retired, err := ti.service.LoadChatOverview(scopedCtx, &gen.LoadChatOverviewPayload{ID: parent.String()})
	require.NoError(t, err)
	require.NotNil(t, retired.RiskFindingsCount)
	require.Zero(t, *retired.RiskFindingsCount, "disabled policies retire findings from overview counts")
	_, err = ti.service.LoadChatOverview(scopedCtx, &gen.LoadChatOverviewPayload{ID: other.String()})
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeForbidden, shareable.Code)
	_, err = ti.service.LoadChatOverview(authztest.WithExactGrants(t, ctx), &gen.LoadChatOverviewPayload{ID: parent.String()})
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeForbidden, shareable.Code)
	after, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionChatSessionAccess)
	require.NoError(t, err)
	require.Equal(t, before, after, "metadata lookup does not open a transcript")
}
