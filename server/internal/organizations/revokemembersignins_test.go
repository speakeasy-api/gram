package organizations_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/signins"
)

func TestService_RevokeMemberSignIns_ForbiddenWithoutOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	workosSessionID := "session_" + uuid.NewString()
	_, err := signins.Record(ctx, ti.conn, signins.RecordParams{OrganizationID: authCtx.ActiveOrganizationID, UserID: testOtherUserID, WorkOSSessionID: workosSessionID, AuthenticationMethod: signins.MethodSSO, AuthenticatedAt: time.Now()})
	require.NoError(t, err)

	denied := authztest.WithExactGrants(t, ctx)
	_, err = ti.service.RevokeMemberSignIns(denied, &gen.RevokeMemberSignInsPayload{SessionToken: nil, UserID: testOtherUserID})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)

	live, err := signins.LiveSignInForSession(ctx, ti.conn, authCtx.ActiveOrganizationID, testOtherUserID, workosSessionID)
	require.NoError(t, err)
	require.True(t, live.Valid, "nothing is revoked without organization admin")
}

func TestService_RevokeMemberSignIns_RevokesOnlyTheActiveOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	flags := &feature.InMemory{}
	signins.EnableForTests(flags, authCtx.ActiveOrganizationID)
	ti.sessionManager.SetFlags(flags)
	here := "session_" + uuid.NewString()
	_, err := signins.Record(ctx, ti.conn, signins.RecordParams{OrganizationID: authCtx.ActiveOrganizationID, UserID: testOtherUserID, WorkOSSessionID: here, AuthenticationMethod: signins.MethodSSO, AuthenticatedAt: time.Now()})
	require.NoError(t, err)

	otherOrg := "org-" + uuid.NewString()
	_, err = orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:             otherOrg,
		Name:           "Other org",
		Slug:           "other-" + uuid.NewString()[:8],
		WorkosID:       pgtype.Text{String: "", Valid: false},
		Whitelisted:    pgtype.Bool{Bool: false, Valid: false},
		CreationSource: pgtype.Text{String: "", Valid: false},
		DefaultHost:    pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	elsewhere := "session_" + uuid.NewString()
	_, err = signins.Record(ctx, ti.conn, signins.RecordParams{OrganizationID: otherOrg, UserID: testOtherUserID, WorkOSSessionID: elsewhere, AuthenticationMethod: signins.MethodSSO, AuthenticatedAt: time.Now()})
	require.NoError(t, err)
	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessMemberSignInsRevoke)
	require.NoError(t, err)

	result, err := ti.service.RevokeMemberSignIns(ctx, &gen.RevokeMemberSignInsPayload{SessionToken: nil, UserID: testOtherUserID})
	require.NoError(t, err)
	require.Equal(t, 1, result.RevokedSignIns)

	gone, err := signins.LiveSignInForSession(ctx, ti.conn, authCtx.ActiveOrganizationID, testOtherUserID, here)
	require.NoError(t, err)
	require.False(t, gone.Valid)
	kept, err := signins.LiveSignInForSession(ctx, ti.conn, otherOrg, testOtherUserID, elsewhere)
	require.NoError(t, err)
	require.True(t, kept.Valid, "another organization's sign-ins are untouched")
	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessMemberSignInsRevoke)
	require.NoError(t, err)
	require.Equal(t, before+1, after, "one summary entry per call")
}
