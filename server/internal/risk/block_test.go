package risk_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	hooksrepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// seedToolCallBlock inserts a durable block row (the same write the hook deny
// path performs) so the risk block endpoints have something to read.
func seedToolCallBlock(t *testing.T, ti *testInstance, orgID string, projectID uuid.UUID, reason, toolName string) uuid.UUID {
	t.Helper()

	id, err := uuid.NewV7()
	require.NoError(t, err)

	require.NoError(t, hooksrepo.New(ti.conn).InsertToolCallBlock(t.Context(), hooksrepo.InsertToolCallBlockParams{
		ID:             id,
		OrganizationID: orgID,
		ProjectID:      projectID,
		Provider:       "claude",
		Reason:         reason,
		ToolName:       pgtype.Text{String: toolName, Valid: toolName != ""},
		RiskPolicyID:   uuid.NullUUID{},
		RiskResultID:   uuid.NullUUID{},
		ChatID:         uuid.NullUUID{},
		ChatMessageID:  uuid.NullUUID{},
	}))

	return id
}

// orgAdminContext grants the active org the org-admin scope. The block
// endpoints no longer require it — a regular member can open their own block
// page (see TestGetRiskBlock_NonAdminMemberCanRead) — but an admin is a valid
// superset caller, so the rest of the cases exercise this path.
func orgAdminContext(t *testing.T, ctx context.Context, ti *testInstance) (context.Context, *contextvalues.AuthContext) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	return ctx, authCtx
}

func TestGetRiskBlock_ReturnsBlock(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx, authCtx := orgAdminContext(t, ctx, ti)

	blockID := seedToolCallBlock(t, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID,
		`Speakeasy blocked this tool call: matched policy "Block Secrets"`, "Bash")

	block, err := ti.service.GetRiskBlock(ctx, &gen.GetRiskBlockPayload{ID: blockID.String()})
	require.NoError(t, err)

	require.Equal(t, blockID.String(), block.ID)
	require.Equal(t, authCtx.ProjectID.String(), block.ProjectID)
	require.Contains(t, block.Reason, "Block Secrets")
	require.NotNil(t, block.ToolName)
	require.Equal(t, "Bash", *block.ToolName)
	require.NotEmpty(t, block.CreatedAt)
	require.Nil(t, block.Feedback, "a fresh block has no feedback recorded")
}

// TestGetRiskBlock_NonAdminMemberCanRead locks in that the durable block page
// is readable by a regular org member, not just org admins: it is opened from
// the link an agent embeds in its block message. The base test context is an
// authenticated session with no org-admin grant.
func TestGetRiskBlock_NonAdminMemberCanRead(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	blockID := seedToolCallBlock(t, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID,
		`Speakeasy blocked this tool call: matched policy "Block Secrets"`, "Bash")

	block, err := ti.service.GetRiskBlock(ctx, &gen.GetRiskBlockPayload{ID: blockID.String()})
	require.NoError(t, err)
	require.Equal(t, blockID.String(), block.ID)
}

// TestGetRiskBlock_MemberWithDifferentActiveOrgCanRead is the core of the
// membership-scoping change: a member of the block's org can read it even when
// their *active* org is a different one. Access keys on org membership, not the
// active org carried in the session.
func TestGetRiskBlock_MemberWithDifferentActiveOrgCanRead(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	blockID := seedToolCallBlock(t, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID,
		`Speakeasy blocked this tool call: matched policy "Block Secrets"`, "Bash")

	// Same member, but their session's active org is now some other org.
	switched := *authCtx
	switched.ActiveOrganizationID = uuid.NewString()
	switchedCtx := contextvalues.SetAuthContext(ctx, &switched)

	block, err := ti.service.GetRiskBlock(switchedCtx, &gen.GetRiskBlockPayload{ID: blockID.String()})
	require.NoError(t, err)
	require.Equal(t, blockID.String(), block.ID)
}

// TestGetRiskBlock_NonMemberDenied confirms a signed-in user who is NOT a member
// of the block's org cannot read it — the plain-UUID link is not usable by
// authenticated outsiders.
func TestGetRiskBlock_NonMemberDenied(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	blockID := seedToolCallBlock(t, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "blocked", "Bash")

	// A different user with no membership in the block's org.
	outsider := *authCtx
	outsider.UserID = uuid.NewString()
	outsiderCtx := contextvalues.SetAuthContext(ctx, &outsider)

	block, err := ti.service.GetRiskBlock(outsiderCtx, &gen.GetRiskBlockPayload{ID: blockID.String()})
	require.Nil(t, block)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestGetRiskBlock_InvalidID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx, _ = orgAdminContext(t, ctx, ti)

	block, err := ti.service.GetRiskBlock(ctx, &gen.GetRiskBlockPayload{ID: "not-a-uuid"})
	require.Nil(t, block)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
}

func TestGetRiskBlock_NotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx, _ = orgAdminContext(t, ctx, ti)

	missing, err := uuid.NewV7()
	require.NoError(t, err)

	block, err := ti.service.GetRiskBlock(ctx, &gen.GetRiskBlockPayload{ID: missing.String()})
	require.Nil(t, block)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestGetRiskBlock_Unauthenticated(t *testing.T) {
	t.Parallel()
	_, ti := newTestRiskService(t)

	missing, err := uuid.NewV7()
	require.NoError(t, err)

	// t.Context() carries no auth context (no session): the block page must be
	// refused before any row is read, so the plain-UUID URL is not public.
	block, err := ti.service.GetRiskBlock(t.Context(), &gen.GetRiskBlockPayload{ID: missing.String()})
	require.Nil(t, block)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestSubmitRiskBlockFeedback_RecordsVote(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx, authCtx := orgAdminContext(t, ctx, ti)

	blockID := seedToolCallBlock(t, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "blocked", "Bash")

	up, err := ti.service.SubmitRiskBlockFeedback(ctx, &gen.SubmitRiskBlockFeedbackPayload{ID: blockID.String(), Sentiment: "up"})
	require.NoError(t, err)
	require.NotNil(t, up.Feedback)
	require.Equal(t, "up", *up.Feedback)

	// A second vote overwrites the first.
	down, err := ti.service.SubmitRiskBlockFeedback(ctx, &gen.SubmitRiskBlockFeedbackPayload{ID: blockID.String(), Sentiment: "down"})
	require.NoError(t, err)
	require.NotNil(t, down.Feedback)
	require.Equal(t, "down", *down.Feedback)

	// And it persists for the next reader.
	got, err := ti.service.GetRiskBlock(ctx, &gen.GetRiskBlockPayload{ID: blockID.String()})
	require.NoError(t, err)
	require.NotNil(t, got.Feedback)
	require.Equal(t, "down", *got.Feedback)
}

func TestSubmitRiskBlockFeedback_InvalidSentiment(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx, authCtx := orgAdminContext(t, ctx, ti)

	blockID := seedToolCallBlock(t, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "blocked", "Bash")

	block, err := ti.service.SubmitRiskBlockFeedback(ctx, &gen.SubmitRiskBlockFeedbackPayload{ID: blockID.String(), Sentiment: "sideways"})
	require.Nil(t, block)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
}

func TestSubmitRiskBlockFeedback_NotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx, _ = orgAdminContext(t, ctx, ti)

	missing, err := uuid.NewV7()
	require.NoError(t, err)

	block, err := ti.service.SubmitRiskBlockFeedback(ctx, &gen.SubmitRiskBlockFeedbackPayload{ID: missing.String(), Sentiment: "up"})
	require.Nil(t, block)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}
