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
	sessionsgen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/ssosignins"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
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
	require.True(t, id.ID.Valid)
	return id.ID.UUID
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
		SsoSignInUserID:        conv.ToPGText(subject.ID),
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
	result, err := ssosignins.CheckSignInIdentity(ctx, f.ti.conn, orgID, sessionID, time.Now(), 0)
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
		{name: "signed-out sign-in", want: ssosignins.ReasonSignInRevoked, prepare: func(t *testing.T, ctx context.Context, f ssoFixture) (string, uuid.UUID) {
			t.Helper()
			workosSessionID := "session_" + uuid.NewString()
			signIn, err := ssosignins.Record(ctx, f.ti.conn, ssosignins.RecordParams{OrganizationID: f.orgID, UserID: f.userID, WorkOSSessionID: workosSessionID, AuthenticationMethod: ssosignins.MethodSSO, AuthenticatedAt: time.Now()})
			require.NoError(t, err)
			session := f.seedSession(t, ctx, urn.NewUserSubject(f.userID), signIn.ID)
			_, err = ssosignins.RevokeSession(ctx, f.ti.conn, []string{f.orgID}, f.userID, workosSessionID)
			require.NoError(t, err)
			return f.orgID, session.ID
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
		{name: "another user's sign-in is never linked", want: ssosignins.ReasonNotLinked, prepare: func(t *testing.T, ctx context.Context, f ssoFixture) (string, uuid.UUID) {
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
	require.False(t, none.ID.Valid, "an exchange without a WorkOS session records nothing")
}

func TestRevokeSession_StopsVouchingButKeepsTheGrant(t *testing.T) {
	t.Parallel()

	ctx, f := newSSOFixture(t)
	params := ssosignins.RecordParams{OrganizationID: f.orgID, UserID: f.userID, WorkOSSessionID: "session_" + uuid.NewString(), AuthenticationMethod: ssosignins.MethodSSO, AuthenticatedAt: time.Now()}
	signIn, err := ssosignins.Record(ctx, f.ti.conn, params)
	require.NoError(t, err)
	session := f.seedSession(t, ctx, urn.NewUserSubject(f.userID), signIn.ID)
	other := f.linkedSession(t, ctx, ssosignins.MethodSSO, time.Now())

	orgs, err := ssosignins.RevokeSession(ctx, f.ti.conn, []string{f.orgID}, f.userID, params.WorkOSSessionID)
	require.NoError(t, err)
	require.Equal(t, []string{f.orgID}, orgs)

	require.Equal(t, ssosignins.ReasonSignInRevoked, f.check(t, ctx, f.orgID, session.ID).Reason)
	require.True(t, f.check(t, ctx, f.orgID, other.ID).Eligible(), "only the signed-out WorkOS session is revoked")
	requireSessionLive(t, ctx, f, session.ID)

	again, err := ssosignins.Record(ctx, f.ti.conn, params)
	require.NoError(t, err)
	require.Equal(t, signIn.ID, again.ID, "a repeated exchange links to the same sign-in")
	require.True(t, again.Revoked, "a repeated exchange never revives a revoked sign-in")
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

func TestRecordSSOSignIn_NewOrganizationKeepsTheSessionsFirstSignInTime(t *testing.T) {
	t.Parallel()

	ctx, f := newSSOFixture(t)
	sessionID := "session_" + uuid.NewString()
	first := time.Now().Add(-6 * 24 * time.Hour)
	otherOrg := seedOtherOrganization(t, ctx, f.ti.conn)
	_, err := ssosignins.Record(ctx, f.ti.conn, ssosignins.RecordParams{OrganizationID: otherOrg, UserID: f.userID, WorkOSSessionID: sessionID, AuthenticationMethod: ssosignins.MethodSSO, AuthenticatedAt: first})
	require.NoError(t, err)

	here, err := ssosignins.Record(ctx, f.ti.conn, ssosignins.RecordParams{OrganizationID: f.orgID, UserID: f.userID, WorkOSSessionID: sessionID, AuthenticationMethod: ssosignins.MethodSSO, AuthenticatedAt: time.Now()})
	require.NoError(t, err)
	session := f.seedSession(t, ctx, urn.NewUserSubject(f.userID), here.ID)

	require.True(t, f.check(t, ctx, f.orgID, session.ID).Eligible())
	later, err := ssosignins.CheckSignInIdentity(ctx, f.ti.conn, f.orgID, session.ID, first.Add(ssosignins.MaxAge+time.Minute), 0)
	require.NoError(t, err)
	require.Equal(t, ssosignins.ReasonExpired, later.Reason, "recording in a second organization does not restart the age window")
}

func TestCheckSignInIdentity_MaxAgeOverride(t *testing.T) {
	t.Parallel()

	ctx, f := newSSOFixture(t)
	session := f.linkedSession(t, ctx, ssosignins.MethodSSO, time.Now().Add(-2*time.Hour))

	result, err := ssosignins.CheckSignInIdentity(ctx, f.ti.conn, f.orgID, session.ID, time.Now(), time.Hour)
	require.NoError(t, err)
	require.Equal(t, ssosignins.ReasonExpired, result.Reason)
}

func TestRevokeUser_RepushesRecentlyRevokedJTIs(t *testing.T) {
	t.Parallel()

	ctx, f := newSSOFixture(t)
	session := f.linkedSession(t, ctx, ssosignins.MethodSSO, time.Now())

	first, err := ssosignins.RevokeUser(ctx, f.ti.conn, []string{f.orgID}, f.userID, ssosignins.RevokeReasonAdmin)
	require.NoError(t, err)
	require.Len(t, first.Sessions, 1)
	requireSessionGone(t, ctx, f, session.ID)

	retry, err := ssosignins.RevokeUser(ctx, f.ti.conn, []string{f.orgID}, f.userID, ssosignins.RevokeReasonAdmin)
	require.NoError(t, err)
	require.Empty(t, retry.Sessions, "nothing left to revoke")
	jtis, err := ssosignins.JTIsToPush(ctx, f.ti.conn, []string{f.orgID}, f.userID, retry, time.Now())
	require.NoError(t, err)
	require.Contains(t, jtis, session.Jti, "a retry re-pushes the earlier revocation's JTIs")
}

func TestCreateUserSession_RefusesAGrantFromARevokedSignIn(t *testing.T) {
	t.Parallel()

	ctx, f := newSSOFixture(t)
	signIn := f.recordSignIn(t, ctx, ssosignins.MethodSSO, time.Now())
	_, err := ssosignins.RevokeUser(ctx, f.ti.conn, []string{f.orgID}, f.userID, ssosignins.RevokeReasonAdmin)
	require.NoError(t, err)

	_, err = createLinkedSession(ctx, f, urn.NewUserSubject(f.userID), signIn)
	require.ErrorIs(t, err, pgx.ErrNoRows, "a grant is never minted from a revoked sign-in")
}

func TestCreateUserSession_KeepsALinkAfterSingleSignOut(t *testing.T) {
	t.Parallel()

	ctx, f := newSSOFixture(t)
	sessionID := "session_" + uuid.NewString()
	signIn, err := ssosignins.Record(ctx, f.ti.conn, ssosignins.RecordParams{OrganizationID: f.orgID, UserID: f.userID, WorkOSSessionID: sessionID, AuthenticationMethod: ssosignins.MethodSSO, AuthenticatedAt: time.Now()})
	require.NoError(t, err)
	_, err = ssosignins.RevokeSession(ctx, f.ti.conn, []string{f.orgID}, f.userID, sessionID)
	require.NoError(t, err)

	row, err := createLinkedSession(ctx, f, urn.NewUserSubject(f.userID), signIn.ID.UUID)
	require.NoError(t, err)
	require.Equal(t, signIn.ID, row.SsoSignInID, "the grant stays reachable by later revocations")
	require.Equal(t, ssosignins.ReasonSignInRevoked, f.check(t, ctx, f.orgID, row.ID).Reason)
}

func TestCreateUserSession_DropsALinkToAnotherUsersSignIn(t *testing.T) {
	t.Parallel()

	ctx, f := newSSOFixture(t)
	signIn := f.recordSignIn(t, ctx, ssosignins.MethodSSO, time.Now())

	row, err := createLinkedSession(ctx, f, urn.NewUserSubject("someone-else"), signIn)
	require.NoError(t, err)
	require.False(t, row.SsoSignInID.Valid)
}

func createLinkedSession(ctx context.Context, f ssoFixture, subject urn.SessionSubject, signIn uuid.UUID) (repo.UserSession, error) {
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
		SsoSignInID:            uuid.NullUUID{UUID: signIn, Valid: true},
		SsoSignInUserID:        conv.ToPGText(subject.ID),
	})
	if err != nil {
		return repo.UserSession{}, err //nolint:wrapcheck // test helper surfaces pgx sentinels
	}
	return row, nil
}

func TestMintUserSession_LinksTheDashboardSignIn(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		impersonated bool
		wantLinked   bool
	}{
		{name: "own sign-in", impersonated: false, wantLinked: true},
		{name: "impersonated session", impersonated: true, wantLinked: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			workosSessionID := "session_" + uuid.NewString()
			current, err := ti.sessionManager.GetSession(ctx, *authCtx.SessionID)
			require.NoError(t, err)
			replacement := current
			replacement.WorkOSSessionID = workosSessionID
			if tc.impersonated {
				replacement.ImpersonatorEmail = "operator@example.com"
			}
			require.NoError(t, ti.sessionManager.UpdateSession(ctx, current, replacement))
			signIn, err := ssosignins.Record(ctx, ti.conn, ssosignins.RecordParams{OrganizationID: authCtx.ActiveOrganizationID, UserID: authCtx.UserID, WorkOSSessionID: workosSessionID, AuthenticationMethod: ssosignins.MethodSSO, AuthenticatedAt: time.Now()})
			require.NoError(t, err)

			toolset := createIssuerGatedMintToolset(t, ctx, ti, "mint-sso-"+uuid.NewString()[:6])
			mintCtx := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPConnect, toolset.ID.String()))
			toolsetID := toolset.ID.String()
			minted, err := ti.service.MintUserSession(mintCtx, &sessionsgen.MintUserSessionPayload{ToolsetID: &toolsetID, McpServerID: nil, SessionToken: nil, ProjectSlugInput: nil})
			require.NoError(t, err)
			claims, err := usersessions.NewSigner("test-jwt-secret").Validate(minted.AccessToken, urn.NewToolset(toolset.ID).String())
			require.NoError(t, err)
			row, err := repo.New(ti.conn).GetUserSessionByJTI(ctx, repo.GetUserSessionByJTIParams{UserSessionIssuerID: toolset.UserSessionIssuerID.UUID, Jti: claims.ID})
			require.NoError(t, err)
			if tc.wantLinked {
				require.Equal(t, signIn.ID, row.SsoSignInID)
			} else {
				require.False(t, row.SsoSignInID.Valid)
			}
		})
	}
}
