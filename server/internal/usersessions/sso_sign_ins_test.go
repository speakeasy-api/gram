package usersessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	gen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/ssosignins"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// ssoFixture is a project issuer in the auth context's organization whose
// verified domains cover the auth context user's email.
type ssoFixture struct {
	ti       *testInstance
	orgID    string
	userID   string
	issuerID uuid.UUID
}

func newSSOFixture(t *testing.T) (context.Context, ssoFixture) {
	t.Helper()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	f := ssoFixture{ti: ti, orgID: authCtx.ActiveOrganizationID, userID: authCtx.UserID, issuerID: seedIssuer(t, ctx, ti, "sso-"+uuid.NewString()[:8])}
	setVerifiedDomains(t, ctx, ti.conn, f.orgID, "example.com")
	return ctx, f
}

func setVerifiedDomains(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, domains ...string) {
	t.Helper()

	_, err := orgrepo.New(conn).UpdateOrganizationMetadataFromWorkOS(ctx, orgrepo.UpdateOrganizationMetadataFromWorkOSParams{
		Name:              mockidp.MockOrgName,
		WorkosID:          conv.ToPGText(mockidp.MockOrgID),
		WorkosUpdatedAt:   pgtype.Timestamptz{Time: time.Now(), InfinityModifier: pgtype.Finite, Valid: true},
		WorkosLastEventID: conv.ToPGText("event_" + uuid.NewString()),
		VerifiedDomains:   domains,
		ID:                orgID,
	})
	require.NoError(t, err)
}

func (f ssoFixture) recordSignIn(t *testing.T, ctx context.Context, method string, at time.Time) uuid.UUID {
	t.Helper()

	id, err := ssosignins.Record(ctx, f.ti.conn, ssosignins.RecordParams{
		OrganizationID:       f.orgID,
		UserID:               f.userID,
		WorkOSSessionID:      "session_" + uuid.NewString(),
		AuthenticationMethod: method,
		AuthenticatedAt:      at,
	})
	require.NoError(t, err)
	require.True(t, id.Valid)
	return id.UUID
}

func (f ssoFixture) seedSession(t *testing.T, ctx context.Context, subject urn.SessionSubject, signInID uuid.NullUUID) repo.UserSession {
	t.Helper()

	now := time.Now()
	row, err := repo.New(f.ti.conn).CreateUserSession(ctx, repo.CreateUserSessionParams{
		UserSessionIssuerID:    f.issuerID,
		UserSessionClientID:    uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		SubjectUrn:             subject,
		AuthorizerUserID:       pgtype.Text{String: "", Valid: false},
		DelegatedGrants:        nil,
		DelegatedGrantsVersion: pgtype.Int4{Int32: 0, Valid: false},
		Jti:                    "jti-" + uuid.NewString(),
		RefreshTokenHash:       conv.ToPGText("hash-" + uuid.NewString()),
		RefreshExpiresAt:       pgtype.Timestamptz{Time: now.Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		ExpiresAt:              pgtype.Timestamptz{Time: now.Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		ToolSelection:          nil,
		Resource:               pgtype.Text{String: "", Valid: false},
		SsoSignInID:            signInID,
	})
	require.NoError(t, err)
	return row
}

func (f ssoFixture) linkedSession(t *testing.T, ctx context.Context, method string, at time.Time) repo.UserSession {
	t.Helper()
	signIn := f.recordSignIn(t, ctx, method, at)
	return f.seedSession(t, ctx, urn.NewUserSubject(f.userID), uuid.NullUUID{UUID: signIn, Valid: true})
}

func (f ssoFixture) check(t *testing.T, ctx context.Context, orgID string, sessionID uuid.UUID) ssosignins.Result {
	t.Helper()
	result, err := ssosignins.CheckSignInIdentity(ctx, f.ti.conn, orgID, sessionID, time.Now())
	require.NoError(t, err)
	return result
}

func TestCheckSignInIdentity_EligibleSSOSignIn(t *testing.T) {
	t.Parallel()

	ctx, f := newSSOFixture(t)
	session := f.linkedSession(t, ctx, ssosignins.MethodSSO, time.Now())

	result := f.check(t, ctx, f.orgID, session.ID)
	require.True(t, result.Eligible(), result.Reason)
	require.Equal(t, f.userID, result.Identity.UserID)
	require.Equal(t, mockidp.MockUserEmail, result.Identity.Email)
	require.Equal(t, session.SsoSignInID.UUID, result.Identity.SignInID)
}

func TestCheckSignInIdentity_RejectsEachFailedRequirement(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		want    ssosignins.Reason
		prepare func(t *testing.T, ctx context.Context, f ssoFixture) (orgID string, sessionID uuid.UUID)
	}{
		{name: "legacy grant without a sign-in", want: ssosignins.ReasonNotLinked, prepare: func(t *testing.T, ctx context.Context, f ssoFixture) (string, uuid.UUID) {
			t.Helper()
			return f.orgID, f.seedSession(t, ctx, urn.NewUserSubject(f.userID), uuid.NullUUID{UUID: uuid.Nil, Valid: false}).ID
		}},
		{name: "magic auth sign-in", want: ssosignins.ReasonNotSSO, prepare: func(t *testing.T, ctx context.Context, f ssoFixture) (string, uuid.UUID) {
			t.Helper()
			return f.orgID, f.linkedSession(t, ctx, "MagicAuth", time.Now()).ID
		}},
		{name: "sign-in older than the maximum age", want: ssosignins.ReasonExpired, prepare: func(t *testing.T, ctx context.Context, f ssoFixture) (string, uuid.UUID) {
			t.Helper()
			return f.orgID, f.linkedSession(t, ctx, ssosignins.MethodSSO, time.Now().Add(-ssosignins.MaxAge-time.Hour)).ID
		}},
		{name: "revoked sign-in", want: ssosignins.ReasonSignInRevoked, prepare: func(t *testing.T, ctx context.Context, f ssoFixture) (string, uuid.UUID) {
			t.Helper()
			session := f.linkedSession(t, ctx, ssosignins.MethodSSO, time.Now())
			_, err := ssosignins.RevokeUser(ctx, f.ti.conn, []string{f.orgID}, f.userID, ssosignins.RevokeReasonAdmin)
			require.NoError(t, err)
			// The cascade soft-deletes the grant; judge a fresh grant on the same sign-in.
			return f.orgID, f.seedSession(t, ctx, urn.NewUserSubject(f.userID), session.SsoSignInID).ID
		}},
		{name: "removed member", want: ssosignins.ReasonNotMember, prepare: func(t *testing.T, ctx context.Context, f ssoFixture) (string, uuid.UUID) {
			t.Helper()
			session := f.linkedSession(t, ctx, ssosignins.MethodSSO, time.Now())
			require.NoError(t, testrepo.New(f.ti.conn).ForceSoftDeleteOrganizationUserRelationshipsFixture(ctx, f.orgID))
			return f.orgID, session.ID
		}},
		{name: "deprovisioned in the directory", want: ssosignins.ReasonDeprovisioned, prepare: func(t *testing.T, ctx context.Context, f ssoFixture) (string, uuid.UUID) {
			t.Helper()
			session := f.linkedSession(t, ctx, ssosignins.MethodSSO, time.Now())
			deprovisionDirectoryUser(t, ctx, f)
			return f.orgID, session.ID
		}},
		{name: "email outside the verified domains", want: ssosignins.ReasonEmailDomain, prepare: func(t *testing.T, ctx context.Context, f ssoFixture) (string, uuid.UUID) {
			t.Helper()
			session := f.linkedSession(t, ctx, ssosignins.MethodSSO, time.Now())
			setVerifiedDomains(t, ctx, f.ti.conn, f.orgID, "other.test")
			return f.orgID, session.ID
		}},
		{name: "no verified domains", want: ssosignins.ReasonEmailDomain, prepare: func(t *testing.T, ctx context.Context, f ssoFixture) (string, uuid.UUID) {
			t.Helper()
			session := f.linkedSession(t, ctx, ssosignins.MethodSSO, time.Now())
			setVerifiedDomains(t, ctx, f.ti.conn, f.orgID)
			return f.orgID, session.ID
		}},
		{name: "disabled organization", want: ssosignins.ReasonOrganizationDisabled, prepare: func(t *testing.T, ctx context.Context, f ssoFixture) (string, uuid.UUID) {
			t.Helper()
			session := f.linkedSession(t, ctx, ssosignins.MethodSSO, time.Now())
			_, err := orgrepo.New(f.ti.conn).DisableOrganizationByWorkosID(ctx, orgrepo.DisableOrganizationByWorkosIDParams{
				WorkosLastEventID: conv.ToPGText("event_" + uuid.NewString()),
				WorkosID:          conv.ToPGText(mockidp.MockOrgID),
			})
			require.NoError(t, err)
			return f.orgID, session.ID
		}},
		{name: "grant for another subject", want: ssosignins.ReasonNotUserSubject, prepare: func(t *testing.T, ctx context.Context, f ssoFixture) (string, uuid.UUID) {
			t.Helper()
			signIn := f.recordSignIn(t, ctx, ssosignins.MethodSSO, time.Now())
			return f.orgID, f.seedSession(t, ctx, urn.NewUserSubject("someone-else"), uuid.NullUUID{UUID: signIn, Valid: true}).ID
		}},
		{name: "checked against another organization", want: ssosignins.ReasonSessionNotFound, prepare: func(t *testing.T, ctx context.Context, f ssoFixture) (string, uuid.UUID) {
			t.Helper()
			return seedOtherOrganization(t, ctx, f.ti.conn), f.linkedSession(t, ctx, ssosignins.MethodSSO, time.Now()).ID
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, f := newSSOFixture(t)
			orgID, sessionID := tc.prepare(t, ctx, f)
			result := f.check(t, ctx, orgID, sessionID)
			require.Equal(t, tc.want, result.Reason)
			require.False(t, result.Eligible())
			require.Nil(t, result.Identity)
		})
	}
}

func deprovisionDirectoryUser(t *testing.T, ctx context.Context, f ssoFixture) {
	t.Helper()

	q := directoryrepo.New(f.ti.conn)
	workosID := "directory_user_" + uuid.NewString()
	now := pgtype.Timestamptz{Time: time.Now(), InfinityModifier: pgtype.Finite, Valid: true}
	_, err := q.UpsertDirectoryUser(ctx, directoryrepo.UpsertDirectoryUserParams{
		OrganizationID:        f.orgID,
		UserID:                conv.ToPGText(f.userID),
		WorkosDirectoryUserID: workosID,
		Email:                 conv.ToPGText(mockidp.MockUserEmail),
		Attributes:            []byte("{}"),
		WorkosCreatedAt:       now,
		WorkosUpdatedAt:       now,
		WorkosLastEventID:     conv.ToPGText("event_" + uuid.NewString()),
		RestoreDeleted:        false,
	})
	require.NoError(t, err)
	_, err = q.DeleteDirectoryUserByWorkOSID(ctx, directoryrepo.DeleteDirectoryUserByWorkOSIDParams{
		WorkosDeletedAt:       now,
		WorkosLastEventID:     conv.ToPGText("event_" + uuid.NewString()),
		WorkosDirectoryUserID: workosID,
	})
	require.NoError(t, err)
}

func seedOtherOrganization(t *testing.T, ctx context.Context, conn *pgxpool.Pool) string {
	t.Helper()

	orgID := "org-" + uuid.NewString()
	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:             orgID,
		Name:           "Other org",
		Slug:           "other-" + uuid.NewString()[:8],
		WorkosID:       pgtype.Text{String: "", Valid: false},
		Whitelisted:    pgtype.Bool{Bool: false, Valid: false},
		CreationSource: pgtype.Text{String: "", Valid: false},
		DefaultHost:    pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	return orgID
}

func TestRecordSSOSignIn_IdempotentPerSessionAndBoundToUser(t *testing.T) {
	t.Parallel()

	ctx, f := newSSOFixture(t)
	params := ssosignins.RecordParams{
		OrganizationID:       f.orgID,
		UserID:               f.userID,
		WorkOSSessionID:      "session_" + uuid.NewString(),
		AuthenticationMethod: ssosignins.MethodSSO,
		AuthenticatedAt:      time.Now(),
	}
	first, err := ssosignins.Record(ctx, f.ti.conn, params)
	require.NoError(t, err)
	again, err := ssosignins.Record(ctx, f.ti.conn, params)
	require.NoError(t, err)
	require.Equal(t, first, again)

	params.UserID = "another-user"
	_, err = ssosignins.Record(ctx, f.ti.conn, params)
	require.Error(t, err, "a WorkOS session never moves to another user")

	params.WorkOSSessionID = ""
	none, err := ssosignins.Record(ctx, f.ti.conn, params)
	require.NoError(t, err)
	require.False(t, none.Valid, "an exchange without a WorkOS session records nothing")
}

func TestRevokeSession_StopsVouchingButKeepsTheGrant(t *testing.T) {
	t.Parallel()

	ctx, f := newSSOFixture(t)
	params := ssosignins.RecordParams{OrganizationID: f.orgID, UserID: f.userID, WorkOSSessionID: "session_" + uuid.NewString(), AuthenticationMethod: ssosignins.MethodSSO, AuthenticatedAt: time.Now()}
	signIn, err := ssosignins.Record(ctx, f.ti.conn, params)
	require.NoError(t, err)
	session := f.seedSession(t, ctx, urn.NewUserSubject(f.userID), signIn)
	other := f.linkedSession(t, ctx, ssosignins.MethodSSO, time.Now())

	n, err := ssosignins.RevokeSession(ctx, f.ti.conn, []string{f.orgID}, f.userID, params.WorkOSSessionID)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	require.Equal(t, ssosignins.ReasonSignInRevoked, f.check(t, ctx, f.orgID, session.ID).Reason)
	require.True(t, f.check(t, ctx, f.orgID, other.ID).Eligible(), "only the signed-out WorkOS session is revoked")
	requireSessionLive(t, ctx, f, session.ID)

	again, err := ssosignins.Record(ctx, f.ti.conn, params)
	require.NoError(t, err)
	require.False(t, again.Valid, "a repeated exchange never revives a revoked sign-in")
}

func TestRevokeMySignIns_RevokesLinkedGrantsEverywhere(t *testing.T) {
	t.Parallel()

	ctx, f := newSSOFixture(t)
	first := f.linkedSession(t, ctx, ssosignins.MethodSSO, time.Now())
	second := f.linkedSession(t, ctx, "MagicAuth", time.Now())
	legacy := f.seedSession(t, ctx, urn.NewUserSubject(f.userID), uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	before, err := audittest.AuditLogCountByAction(ctx, f.ti.conn, audit.ActionUserSessionRevoke)
	require.NoError(t, err)

	result, err := f.ti.service.RevokeMySignIns(ctx, &gen.RevokeMySignInsPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, 2, result.RevokedSignIns)
	require.Equal(t, 2, result.RevokedSessions)

	requireSessionGone(t, ctx, f, first.ID)
	requireSessionGone(t, ctx, f, second.ID)
	requireSessionLive(t, ctx, f, legacy.ID)
	require.True(t, jtiRevoked(t, ctx, f.ti.redis, first.Jti))
	require.True(t, jtiRevoked(t, ctx, f.ti.redis, second.Jti))
	require.False(t, jtiRevoked(t, ctx, f.ti.redis, legacy.Jti))

	after, err := audittest.AuditLogCountByAction(ctx, f.ti.conn, audit.ActionUserSessionRevoke)
	require.NoError(t, err)
	require.Equal(t, before+2, after, "one audit entry per revoked grant")
}

func TestRevokeMemberSignIns_RequiresOrganizationAdmin(t *testing.T) {
	t.Parallel()

	ctx, f := newSSOFixture(t)
	session := f.linkedSession(t, ctx, ssosignins.MethodSSO, time.Now())

	denied := withExactAuthzGrants(t, ctx, f.ti.conn)
	_, err := f.ti.service.RevokeMemberSignIns(denied, &gen.RevokeMemberSignInsPayload{SessionToken: nil, UserID: f.userID})
	requireOopsCode(t, err, oops.CodeForbidden)
	requireSessionLive(t, ctx, f, session.ID)
}

func TestRevokeMemberSignIns_RevokesOnlyTheActiveOrganization(t *testing.T) {
	t.Parallel()

	ctx, f := newSSOFixture(t)
	session := f.linkedSession(t, ctx, ssosignins.MethodSSO, time.Now())
	otherOrg := seedOtherOrganization(t, ctx, f.ti.conn)
	otherSession := "session_" + uuid.NewString()
	elsewhere, err := ssosignins.Record(ctx, f.ti.conn, ssosignins.RecordParams{OrganizationID: otherOrg, UserID: f.userID, WorkOSSessionID: otherSession, AuthenticationMethod: ssosignins.MethodSSO, AuthenticatedAt: time.Now()})
	require.NoError(t, err)
	require.True(t, elsewhere.Valid)

	result, err := f.ti.service.RevokeMemberSignIns(ctx, &gen.RevokeMemberSignInsPayload{SessionToken: nil, UserID: f.userID})
	require.NoError(t, err)
	require.Equal(t, 1, result.RevokedSignIns)
	require.Equal(t, 1, result.RevokedSessions)
	requireSessionGone(t, ctx, f, session.ID)

	still, err := ssosignins.LiveSignInForSession(ctx, f.ti.conn, otherOrg, f.userID, otherSession)
	require.NoError(t, err)
	require.Equal(t, elsewhere, still, "another organization's sign-ins are untouched")
}

func requireSessionLive(t *testing.T, ctx context.Context, f ssoFixture, id uuid.UUID) {
	t.Helper()
	_, err := ssoSessionByID(ctx, f, id)
	require.NoError(t, err)
}

func requireSessionGone(t *testing.T, ctx context.Context, f ssoFixture, id uuid.UUID) {
	t.Helper()
	_, err := ssoSessionByID(ctx, f, id)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func ssoSessionByID(ctx context.Context, f ssoFixture, id uuid.UUID) (repo.UserSession, error) {
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	row, err := repo.New(f.ti.conn).GetUserSessionByID(ctx, repo.GetUserSessionByIDParams{ID: id, ProjectID: *authCtx.ProjectID, OrganizationID: f.orgID})
	if err != nil {
		return repo.UserSession{}, err //nolint:wrapcheck // test helper surfaces pgx sentinels
	}
	return row, nil
}
