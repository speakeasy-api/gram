package admin

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func TestListPlatformMcpFeedback(t *testing.T) {
	t.Parallel()
	ctx, service, db := newTestAdminService(t)

	_, err := service.ListPlatformMcpFeedback(ctx, &gen.ListPlatformMcpFeedbackPayload{Limit: 200})
	require.Error(t, err, "requires an admin session")

	const orgID = "org_feedback_fixture"
	_, err = orgrepo.New(db).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: orgID, Name: "Feedback Fixture", Slug: "feedback-fixture", WorkosID: conv.ToPGText(orgID), Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)
	_, err = usersrepo.New(db).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID: "user_feedback_fixture", Email: "feedback@example.test", DisplayName: "Feedback Fixture", PhotoUrl: pgtype.Text{String: "", Valid: false}, Admin: false,
	})
	require.NoError(t, err)

	now := time.Now()
	at := func(d time.Duration) pgtype.Timestamptz {
		return pgtype.Timestamptz{Time: now.Add(d), Valid: true, InfinityModifier: pgtype.Finite}
	}
	q := repo.New(db)
	for _, row := range []repo.AdminInsertPlatformMCPFeedbackFixtureParams{
		{OrganizationID: orgID, SubjectUrn: "user:user_feedback_fixture", Category: "missing_capability", Rating: pgtype.Int4{Int32: 2, Valid: true}, ToolName: conv.ToPGText("find_mcp"), Note: conv.ToPGText("Could not find the server I wanted"), IdempotencyKey: "key-old", CreatedAt: at(-time.Hour), ExpiresAt: at(24 * time.Hour)},
		{OrganizationID: orgID, SubjectUrn: "user:user_gone", Category: "success", Rating: pgtype.Int4{Int32: 0, Valid: false}, ToolName: pgtype.Text{String: "", Valid: false}, Note: pgtype.Text{String: "", Valid: false}, IdempotencyKey: "key-new", CreatedAt: at(0), ExpiresAt: at(24 * time.Hour)},
		{OrganizationID: orgID, SubjectUrn: "user:user_feedback_fixture", Category: "other", Rating: pgtype.Int4{Int32: 0, Valid: false}, ToolName: pgtype.Text{String: "", Valid: false}, Note: pgtype.Text{String: "", Valid: false}, IdempotencyKey: "key-expired", CreatedAt: at(0), ExpiresAt: at(-time.Second)},
	} {
		require.NoError(t, q.AdminInsertPlatformMCPFeedbackFixture(ctx, row))
	}

	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{Email: "operator@example.test"})
	result, err := service.ListPlatformMcpFeedback(ctx, &gen.ListPlatformMcpFeedbackPayload{Limit: 200})
	require.NoError(t, err)
	require.Len(t, result.Feedback, 2, "expired rows are hidden")

	newest, oldest := result.Feedback[0], result.Feedback[1]
	require.Equal(t, "success", newest.Category)
	require.Nil(t, newest.UserEmail, "a deleted user has no email")
	require.Nil(t, newest.Rating)

	require.Equal(t, "missing_capability", oldest.Category)
	require.Equal(t, orgID, oldest.OrganizationID)
	require.Equal(t, "Feedback Fixture", *oldest.OrganizationName)
	require.Equal(t, "feedback-fixture", *oldest.OrganizationSlug)
	require.Equal(t, "feedback@example.test", *oldest.UserEmail)
	require.Equal(t, 2, *oldest.Rating)
	require.Equal(t, "find_mcp", *oldest.ToolName)
	require.Equal(t, "Could not find the server I wanted", *oldest.Note)
	require.Equal(t, "queued", oldest.DeliveryState)

	limited, err := service.ListPlatformMcpFeedback(ctx, &gen.ListPlatformMcpFeedbackPayload{Limit: 1})
	require.NoError(t, err)
	require.Len(t, limited.Feedback, 1)
	require.Equal(t, newest.ID, limited.Feedback[0].ID)
}
