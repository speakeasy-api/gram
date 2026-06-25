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

// orgAdminContext grants the active org the org-admin scope the block endpoints
// require, mirroring the gate on the rest of the risk surface.
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
