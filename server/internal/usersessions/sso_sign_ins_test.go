package usersessions_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
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
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/ssosignins"
	ssosigninsrepo "github.com/speakeasy-api/gram/server/internal/ssosignins/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
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

const workTenant = "00000000-0000-4000-8000-0000000000aa"

// evidence is what WorkOS vouched for: the fixture user's verified email and
// an authentication at the given time.
func evidence(mfa bool, hostedDomain, tenantID string, authenticatedAt time.Time) ssosignins.Evidence {
	return ssosignins.Evidence{MFAVerified: mfa, HostedDomain: hostedDomain, TenantID: tenantID, VerifiedEmail: mockidp.MockUserEmail, IDPAuthenticatedAt: authenticatedAt}
}

// signInParams records an SSO sign-in that authenticated at at.
func (f ssoFixture) signInParams(workosSessionID string, at time.Time) ssosignins.RecordParams {
	return ssosignins.RecordParams{
		OrganizationID:       f.orgID,
		UserID:               f.userID,
		WorkOSSessionID:      workosSessionID,
		AuthenticationMethod: ssosignins.MethodSSO,
		Evidence:             evidence(false, "", "", at),
		AuthenticatedAt:      at,
	}
}

func (f ssoFixture) recordSignIn(t *testing.T, ctx context.Context, method string, at time.Time) uuid.UUID {
	t.Helper()
	return f.recordSignInWith(t, ctx, method, evidence(false, "", "", at), at)
}

func (f ssoFixture) recordSignInWith(t *testing.T, ctx context.Context, method string, ev ssosignins.Evidence, at time.Time) uuid.UUID {
	t.Helper()

	params := f.signInParams("session_"+uuid.NewString(), at)
	params.AuthenticationMethod = method
	params.Evidence = ev
	id, err := ssosignins.Record(ctx, f.ti.conn, params)
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

func (f ssoFixture) linkedSessionWith(t *testing.T, ctx context.Context, method string, ev ssosignins.Evidence, at time.Time) repo.UserSession {
	t.Helper()
	signIn := f.recordSignInWith(t, ctx, method, ev, at)
	return f.seedSession(t, ctx, urn.NewUserSubject(f.userID), uuid.NullUUID{UUID: signIn, Valid: true})
}

func (f ssoFixture) limitSignInMethods(t *testing.T, ctx context.Context, tiers ...string) {
	t.Helper()
	f.seedIssuer(t, ctx, tiers, []string{workTenant})
}

// seedIssuer plants the organization's internal issuer; nil tiers allow all.
func (f ssoFixture) seedIssuer(t *testing.T, ctx context.Context, tiers, microsoftTenants []string) {
	t.Helper()
	require.NoError(t, testrepo.New(f.ti.conn).SeedInternalSessionIssuerSignInMethodsFixture(ctx, testrepo.SeedInternalSessionIssuerSignInMethodsFixtureParams{
		OrganizationID:            f.orgID,
		Issuer:                    "https://issuer.example.com/oauth/idp/org/" + f.orgID,
		AllowedSignInMethods:      tiers,
		AllowedMicrosoftTenantIds: microsoftTenants,
		Name:                      conv.ToPGText("issuer-" + uuid.NewString()[:8]),
	}))
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

func TestCheckSignInIdentity_AcceptsEachTierByDefault(t *testing.T) {
	t.Parallel()

	now := time.Now()
	none := evidence(false, "", "", now)
	for _, tc := range []struct {
		name     string
		method   string
		evidence ssosignins.Evidence
		tier     string
	}{
		{name: "sso", method: ssosignins.MethodSSO, evidence: none, tier: ssosignins.TierSSO},
		{name: "google workspace", method: ssosignins.MethodGoogleOAuth, evidence: evidence(false, "example.com", "", now), tier: ssosignins.TierGoogle},
		{name: "microsoft work account in a pinned tenant", method: ssosignins.MethodMicrosoftOAuth, evidence: evidence(false, "example.com", workTenant, now), tier: ssosignins.TierMicrosoft},
		{name: "magic link", method: ssosignins.MethodMagicAuth, evidence: none, tier: ssosignins.TierMagicLink},
		{name: "password with mfa", method: ssosignins.MethodPassword, evidence: evidence(true, "", "", now), tier: ssosignins.TierPasswordMFA},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, f := newSSOFixture(t)
			f.seedIssuer(t, ctx, nil, []string{workTenant})
			session := f.linkedSessionWith(t, ctx, tc.method, tc.evidence, now)
			result := f.check(t, ctx, f.orgID, session.ID)
			require.True(t, result.Eligible(), result.Reason)
			require.Equal(t, tc.tier, result.Identity.Tier)
		})
	}
}

func TestCheckSignInIdentity_HonoursTheTiersTheAdminKept(t *testing.T) {
	t.Parallel()

	ctx, f := newSSOFixture(t)
	f.limitSignInMethods(t, ctx, ssosignins.TierSSO, ssosignins.TierMagicLink)
	require.True(t, f.check(t, ctx, f.orgID, f.linkedSession(t, ctx, ssosignins.MethodSSO, time.Now()).ID).Eligible())
	require.True(t, f.check(t, ctx, f.orgID, f.linkedSession(t, ctx, ssosignins.MethodMagicAuth, time.Now()).ID).Eligible())
	password := f.linkedSessionWith(t, ctx, ssosignins.MethodPassword, evidence(true, "", "", time.Now()), time.Now())
	require.Equal(t, ssosignins.ReasonMethodNotAllowed, f.check(t, ctx, f.orgID, password.ID).Reason)
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
		{name: "password sign-in without mfa", want: ssosignins.ReasonMFARequired, prepare: func(t *testing.T, ctx context.Context, f ssoFixture) (string, uuid.UUID) {
			t.Helper()
			return f.orgID, f.linkedSession(t, ctx, ssosignins.MethodPassword, time.Now()).ID
		}},
		{name: "personal google account on the company email", want: ssosignins.ReasonUnmanagedAccount, prepare: func(t *testing.T, ctx context.Context, f ssoFixture) (string, uuid.UUID) {
			t.Helper()
			return f.orgID, f.linkedSessionWith(t, ctx, ssosignins.MethodGoogleOAuth, evidence(false, "", "", time.Now()), time.Now()).ID
		}},
		{name: "microsoft tenant the organization never pinned", want: ssosignins.ReasonTenantNotAllowed, prepare: func(t *testing.T, ctx context.Context, f ssoFixture) (string, uuid.UUID) {
			t.Helper()
			return f.orgID, f.linkedSessionWith(t, ctx, ssosignins.MethodMicrosoftOAuth, evidence(false, "example.com", workTenant, time.Now()), time.Now()).ID
		}},
		{name: "email the identity provider did not verify", want: ssosignins.ReasonEmailNotVerified, prepare: func(t *testing.T, ctx context.Context, f ssoFixture) (string, uuid.UUID) {
			t.Helper()
			ev := evidence(false, "", "", time.Now())
			ev.VerifiedEmail = ""
			return f.orgID, f.linkedSessionWith(t, ctx, ssosignins.MethodSSO, ev, time.Now()).ID
		}},
		{name: "profile email changed after sign-in", want: ssosignins.ReasonEmailChanged, prepare: func(t *testing.T, ctx context.Context, f ssoFixture) (string, uuid.UUID) {
			t.Helper()
			session := f.linkedSession(t, ctx, ssosignins.MethodSSO, time.Now())
			require.NoError(t, usersrepo.New(f.ti.conn).SetOrganizationUserEmailFixture(ctx, usersrepo.SetOrganizationUserEmailFixtureParams{Email: "renamed@example.com", UserID: f.userID, OrganizationID: f.orgID}))
			return f.orgID, session.ID
		}},
		{name: "authentication time unknown", want: ssosignins.ReasonAuthTimeUnknown, prepare: func(t *testing.T, ctx context.Context, f ssoFixture) (string, uuid.UUID) {
			t.Helper()
			ev := evidence(false, "", "", time.Time{})
			return f.orgID, f.linkedSessionWith(t, ctx, ssosignins.MethodSSO, ev, time.Now()).ID
		}},
		{name: "magic link older than its tier allows", want: ssosignins.ReasonExpired, prepare: func(t *testing.T, ctx context.Context, f ssoFixture) (string, uuid.UUID) {
			t.Helper()
			return f.orgID, f.linkedSession(t, ctx, ssosignins.MethodMagicAuth, time.Now().Add(-5*time.Hour)).ID
		}},
		{name: "tier turned off by the admin", want: ssosignins.ReasonMethodNotAllowed, prepare: func(t *testing.T, ctx context.Context, f ssoFixture) (string, uuid.UUID) {
			t.Helper()
			f.limitSignInMethods(t, ctx, ssosignins.TierSSO)
			return f.orgID, f.linkedSession(t, ctx, ssosignins.MethodMagicAuth, time.Now()).ID
		}},
		{name: "sign-in older than the maximum age", want: ssosignins.ReasonExpired, prepare: func(t *testing.T, ctx context.Context, f ssoFixture) (string, uuid.UUID) {
			t.Helper()
			return f.orgID, f.linkedSession(t, ctx, ssosignins.MethodSSO, time.Now().Add(-ssosignins.MaxAge-time.Hour)).ID
		}},
		{name: "signed-out sign-in", want: ssosignins.ReasonSignInRevoked, prepare: func(t *testing.T, ctx context.Context, f ssoFixture) (string, uuid.UUID) {
			t.Helper()
			workosSessionID := "session_" + uuid.NewString()
			signIn, err := ssosignins.Record(ctx, f.ti.conn, f.signInParams(workosSessionID, time.Now()))
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
	params := f.signInParams("session_"+uuid.NewString(), time.Now())
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
	params := f.signInParams("session_"+uuid.NewString(), time.Now())
	signIn, err := ssosignins.Record(ctx, f.ti.conn, params)
	require.NoError(t, err)
	session := f.seedSession(t, ctx, urn.NewUserSubject(f.userID), signIn.ID)
	other := f.linkedSession(t, ctx, ssosignins.MethodSSO, time.Now())

	orgs, err := ssosignins.RevokeSession(ctx, f.ti.conn, []string{f.orgID}, f.userID, params.WorkOSSessionID)
	require.NoError(t, err)
	require.Equal(t, []string{f.orgID}, orgs.Organizations)

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
	elsewhere := f.signInParams(sessionID, first)
	elsewhere.OrganizationID = otherOrg
	_, err := ssosignins.Record(ctx, f.ti.conn, elsewhere)
	require.NoError(t, err)

	here, err := ssosignins.Record(ctx, f.ti.conn, f.signInParams(sessionID, time.Now()))
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

type recordingRevoker struct {
	mu      sync.Mutex
	calls   []string
	fail    bool
	ctxErrs []error
}

func (r *recordingRevoker) revoke(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, id)
	r.ctxErrs = append(r.ctxErrs, ctx.Err())
	if r.fail {
		return errors.New("unavailable")
	}
	return nil
}

func (r *recordingRevoker) RevokeToken(ctx context.Context, jti string) error {
	return r.revoke(ctx, jti)
}

func (r *recordingRevoker) RevokeSession(ctx context.Context, id string) error {
	return r.revoke(ctx, id)
}

func pendingDeliveries(t *testing.T, ctx context.Context, f ssoFixture) int {
	t.Helper()
	n, err := testrepo.New(f.ti.conn).CountPendingSSOSignInRevocationDeliveries(ctx, f.orgID)
	require.NoError(t, err)
	return int(n)
}

func newDeliverer(t *testing.T, f ssoFixture, revoker *recordingRevoker, epochs ssosignins.EpochRaiser) *ssosignins.Deliverer {
	t.Helper()
	return ssosignins.NewDeliverer(testenv.NewLogger(t), testenv.NewMeterProvider(t), f.ti.conn, revoker, revoker, epochs)
}

func deliveries(t *testing.T, ctx context.Context, f ssoFixture) []testrepo.ListSSOSignInRevocationDeliveriesFixtureRow {
	t.Helper()
	rows, err := testrepo.New(f.ti.conn).ListSSOSignInRevocationDeliveriesFixture(ctx, f.orgID)
	require.NoError(t, err)
	return rows
}

func TestRevokeUser_DeliveriesSurviveAFailedPushAndTheSweepRetriesThem(t *testing.T) {
	t.Parallel()

	ctx, f := newSSOFixture(t)
	session := f.linkedSession(t, ctx, ssosignins.MethodSSO, time.Now())

	revoked, err := ssosignins.RevokeUser(ctx, f.ti.conn, []string{f.orgID}, f.userID, ssosignins.RevokeReasonAdmin)
	require.NoError(t, err)
	require.Len(t, revoked.DeliveryIDs, 2, "one JTI and one WorkOS session delivery")
	requireSessionGone(t, ctx, f, session.ID)

	down := &recordingRevoker{mu: sync.Mutex{}, calls: nil, fail: true, ctxErrs: nil}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	failing := newDeliverer(t, f, down, nil)
	failing.DeliverNow(cancelled, []string{f.orgID}, revoked.DeliveryIDs)
	failing.Wait()
	require.ElementsMatch(t, []string{session.Jti, revoked.WorkOSSessionIDs[0]}, down.calls)
	for _, err := range down.ctxErrs {
		require.NoError(t, err, "a cancelled request never cancels the pushes")
	}
	require.Equal(t, 2, pendingDeliveries(t, ctx, f), "failed pushes stay pending")
	for _, d := range deliveries(t, ctx, f) {
		require.Equal(t, int32(1), d.Attempts)
		require.True(t, d.NextAttemptAt.Valid && d.NextAttemptAt.Time.After(time.Now()), "a failure backs off")
		require.False(t, d.ClaimedUntil.Valid, "a finished attempt releases its lease")
	}

	up := &recordingRevoker{mu: sync.Mutex{}, calls: nil, fail: false, ctxErrs: nil}
	deliverer := newDeliverer(t, f, up, nil)
	claimed, err := deliverer.Sweep(ctx)
	require.NoError(t, err)
	require.NotContains(t, up.calls, session.Jti, "the sweep waits out the backoff")
	_ = claimed

	require.NoError(t, testrepo.New(f.ti.conn).MakeSSOSignInRevocationDeliveriesDueFixture(ctx, f.orgID))
	for pendingDeliveries(t, ctx, f) > 0 {
		claimed, err := deliverer.Sweep(ctx)
		require.NoError(t, err)
		require.Positive(t, claimed, "the sweep reaches this organization's deliveries")
	}
	require.Subset(t, up.calls, []string{session.Jti, revoked.WorkOSSessionIDs[0]})
}

func TestDeliveries_ALeaseKeepsOtherWorkersOff(t *testing.T) {
	t.Parallel()

	ctx, f := newSSOFixture(t)
	f.linkedSession(t, ctx, ssosignins.MethodSSO, time.Now())
	revoked, err := ssosignins.RevokeUser(ctx, f.ti.conn, []string{f.orgID}, f.userID, ssosignins.RevokeReasonAdmin)
	require.NoError(t, err)

	q := ssosigninsrepo.New(f.ti.conn)
	first, err := q.ClaimRevocationDeliveries(ctx, ssosigninsrepo.ClaimRevocationDeliveriesParams{LeaseSeconds: 60, OrganizationIds: []string{f.orgID}, Ids: revoked.DeliveryIDs})
	require.NoError(t, err)
	require.Len(t, first, 2)
	second, err := q.ClaimRevocationDeliveries(ctx, ssosigninsrepo.ClaimRevocationDeliveriesParams{LeaseSeconds: 60, OrganizationIds: []string{f.orgID}, Ids: revoked.DeliveryIDs})
	require.NoError(t, err)
	require.Empty(t, second, "a leased delivery is not claimed twice")

	require.NoError(t, q.MarkRevocationDelivered(ctx, ssosigninsrepo.MarkRevocationDeliveredParams{ID: first[0].ID, OrganizationID: f.orgID, EffectiveAt: first[0].EffectiveAt}))
	require.Equal(t, 1, pendingDeliveries(t, ctx, f), "each delivery is acknowledged on its own")
}

type recordingEpochs struct {
	mu  sync.Mutex
	got map[string]time.Time
}

func (r *recordingEpochs) RaiseUserEpoch(_ context.Context, userID string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got[userID] = at
	return nil
}

func TestDeliveries_AUserEpochOnlyMovesForward(t *testing.T) {
	t.Parallel()

	ctx, f := newSSOFixture(t)
	earlier := time.Now().Add(-time.Minute).Truncate(time.Microsecond)
	later := earlier.Add(30 * time.Second)
	first, err := ssosignins.EnqueueUserEpoch(ctx, f.ti.conn, f.orgID, f.userID, later)
	require.NoError(t, err)
	again, err := ssosignins.EnqueueUserEpoch(ctx, f.ti.conn, f.orgID, f.userID, earlier)
	require.NoError(t, err)
	require.Equal(t, first, again, "a pending epoch is reused")

	epochs := &recordingEpochs{mu: sync.Mutex{}, got: map[string]time.Time{}}
	deliverer := newDeliverer(t, f, &recordingRevoker{mu: sync.Mutex{}, calls: nil, fail: false, ctxErrs: nil}, epochs)
	deliverer.DeliverNow(ctx, []string{f.orgID}, []uuid.UUID{first})
	deliverer.Wait()
	require.True(t, later.Equal(epochs.got[f.userID]), "the later epoch wins")
	require.Equal(t, 0, pendingDeliveries(t, ctx, f))
}

func TestRecordSSOSignIn_SilentSessionReuseKeepsTheAuthenticationTime(t *testing.T) {
	t.Parallel()

	ctx, f := newSSOFixture(t)
	workosSessionID := "session_" + uuid.NewString()
	authenticated := time.Now().Add(-6 * 24 * time.Hour)
	first, err := ssosignins.Record(ctx, f.ti.conn, f.signInParams(workosSessionID, authenticated))
	require.NoError(t, err)

	// AuthKit hands the same session to a later authorization without a new
	// authentication; the exchange time is now but WorkOS reports the old one.
	reuse := f.signInParams(workosSessionID, time.Now())
	reuse.Evidence.IDPAuthenticatedAt = time.Now()
	again, err := ssosignins.Record(ctx, f.ti.conn, reuse)
	require.NoError(t, err)
	require.Equal(t, first.ID, again.ID)

	session := f.seedSession(t, ctx, urn.NewUserSubject(f.userID), again.ID)
	later, err := ssosignins.CheckSignInIdentity(ctx, f.ti.conn, f.orgID, session.ID, authenticated.Add(ssosignins.MaxAge+time.Minute), 0)
	require.NoError(t, err)
	require.Equal(t, ssosignins.ReasonExpired, later.Reason, "reusing a session never restarts its age")
}

func TestRecordSSOSignIn_FillsEvidenceAMissedLookupLeftOut(t *testing.T) {
	t.Parallel()

	ctx, f := newSSOFixture(t)
	workosSessionID := "session_" + uuid.NewString()
	missed := f.signInParams(workosSessionID, time.Now())
	missed.AuthenticationMethod = ssosignins.MethodGoogleOAuth
	first, err := ssosignins.Record(ctx, f.ti.conn, missed)
	require.NoError(t, err)
	session := f.seedSession(t, ctx, urn.NewUserSubject(f.userID), first.ID)
	require.Equal(t, ssosignins.ReasonUnmanagedAccount, f.check(t, ctx, f.orgID, session.ID).Reason)

	found := missed
	found.Evidence.HostedDomain = "example.com"
	_, err = ssosignins.Record(ctx, f.ti.conn, found)
	require.NoError(t, err)
	require.True(t, f.check(t, ctx, f.orgID, session.ID).Eligible(), "a later exchange of the same method fills the gap")

	other := missed
	other.AuthenticationMethod = ssosignins.MethodSSO
	other.Evidence.MFAVerified = true
	_, err = ssosignins.Record(ctx, f.ti.conn, other)
	require.NoError(t, err)
	require.Equal(t, ssosignins.TierGoogle, f.check(t, ctx, f.orgID, session.ID).Identity.Tier, "another method's evidence never lands")
}

func TestRecord_WaitsForAConcurrentRevocation(t *testing.T) {
	t.Parallel()

	ctx, f := newSSOFixture(t)
	startedBefore := time.Now()

	tx, err := f.ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: holds the revocation open so a second connection can interleave
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.WithoutCancel(ctx)) })
	_, err = ssosignins.RevokeUser(ctx, tx, []string{f.orgID}, f.userID, ssosignins.RevokeReasonSignOutEverywhere)
	require.NoError(t, err)

	type outcome struct {
		recorded ssosignins.Recorded
		err      error
	}
	done := make(chan outcome, 1)
	go func() {
		recorded, err := ssosignins.Record(ctx, f.ti.conn, f.signInParams("session_"+uuid.NewString(), startedBefore))
		done <- outcome{recorded: recorded, err: err}
	}()
	select {
	case <-done:
		t.Fatal("recording ran while the revocation held the user's lock")
	case <-time.After(300 * time.Millisecond):
	}
	require.NoError(t, tx.Commit(ctx))
	got := <-done
	require.NoError(t, got.err)
	require.True(t, got.recorded.Revoked, "the recording that waited is fenced by the committed cutoff")
}

func TestRevokeSession_FencesASignInRecordedAfterwards(t *testing.T) {
	t.Parallel()

	ctx, f := newSSOFixture(t)
	workosSessionID := "session_" + uuid.NewString()
	_, err := ssosignins.RevokeSession(ctx, f.ti.conn, []string{f.orgID}, f.userID, workosSessionID)
	require.NoError(t, err)

	late, err := ssosignins.Record(ctx, f.ti.conn, f.signInParams(workosSessionID, time.Now()))
	require.NoError(t, err)
	require.True(t, late.Revoked, "an exchange that lands after sign-out is recorded revoked")

	otherOrg := seedOtherOrganization(t, ctx, f.ti.conn)
	elsewhere := f.signInParams(workosSessionID, time.Now())
	elsewhere.OrganizationID = otherOrg
	inherited, err := ssosignins.Record(ctx, f.ti.conn, elsewhere)
	require.NoError(t, err)
	require.True(t, inherited.Revoked, "and so is the same session recorded in another organization")
}

func TestRevokeUser_KeepsAWorkOSSessionAnotherOrganizationStillUses(t *testing.T) {
	t.Parallel()

	ctx, f := newSSOFixture(t)
	shared := "session_" + uuid.NewString()
	_, err := ssosignins.Record(ctx, f.ti.conn, f.signInParams(shared, time.Now()))
	require.NoError(t, err)
	otherOrg := seedOtherOrganization(t, ctx, f.ti.conn)
	elsewhere := f.signInParams(shared, time.Now())
	elsewhere.OrganizationID = otherOrg
	_, err = ssosignins.Record(ctx, f.ti.conn, elsewhere)
	require.NoError(t, err)
	own := "session_" + uuid.NewString()
	_, err = ssosignins.Record(ctx, f.ti.conn, f.signInParams(own, time.Now()))
	require.NoError(t, err)

	revoked, err := ssosignins.RevokeUser(ctx, f.ti.conn, []string{f.orgID}, f.userID, ssosignins.RevokeReasonAdmin)
	require.NoError(t, err)
	require.Equal(t, 2, revoked.SignIns())
	var ended []string
	for _, d := range deliveries(t, ctx, f) {
		if d.Kind == string(ssosignins.DeliveryWorkOSSession) {
			ended = append(ended, d.Target)
		}
	}
	require.Equal(t, []string{own}, ended, "a WorkOS session still backing another organization stays up")

	stillLive, err := ssosignins.LiveSignInForSession(ctx, f.ti.conn, otherOrg, f.userID, shared)
	require.NoError(t, err)
	require.True(t, stillLive.Valid, "only this organization's rows are revoked")
}

func TestRevokeUser_UpgradesASignedOutSignIn(t *testing.T) {
	t.Parallel()

	ctx, f := newSSOFixture(t)
	sessionID := "session_" + uuid.NewString()
	signIn, err := ssosignins.Record(ctx, f.ti.conn, f.signInParams(sessionID, time.Now()))
	require.NoError(t, err)
	_, err = ssosignins.RevokeSession(ctx, f.ti.conn, []string{f.orgID}, f.userID, sessionID)
	require.NoError(t, err)
	_, err = createLinkedSession(ctx, f, urn.NewUserSubject(f.userID), signIn.ID.UUID)
	require.NoError(t, err, "a signed-out sign-in still links")

	revoked, err := ssosignins.RevokeUser(ctx, f.ti.conn, []string{f.orgID}, f.userID, ssosignins.RevokeReasonAdmin)
	require.NoError(t, err)
	require.Equal(t, 1, revoked.SignIns(), "a broad revocation upgrades a signed-out sign-in")
	require.Len(t, revoked.Sessions, 1)

	_, err = createLinkedSession(ctx, f, urn.NewUserSubject(f.userID), signIn.ID.UUID)
	require.ErrorIs(t, err, pgx.ErrNoRows, "outstanding codes and refreshes from it can no longer mint")
}

func TestRecord_AnExchangeStartedBeforeABroadRevocationIsFencedOut(t *testing.T) {
	t.Parallel()

	ctx, f := newSSOFixture(t)
	startedBefore := time.Now()
	_, err := ssosignins.RevokeUser(ctx, f.ti.conn, []string{f.orgID}, f.userID, ssosignins.RevokeReasonSignOutEverywhere)
	require.NoError(t, err)

	late, err := ssosignins.Record(ctx, f.ti.conn, f.signInParams("session_"+uuid.NewString(), startedBefore))
	require.NoError(t, err)
	require.True(t, late.Revoked, "a sign-in in flight during the revocation is recorded revoked")
	_, err = createLinkedSession(ctx, f, urn.NewUserSubject(f.userID), late.ID.UUID)
	require.ErrorIs(t, err, pgx.ErrNoRows)

	fresh, err := ssosignins.Record(ctx, f.ti.conn, f.signInParams("session_"+uuid.NewString(), time.Now()))
	require.NoError(t, err)
	require.False(t, fresh.Revoked, "a later sign-in works")
	_, err = createLinkedSession(ctx, f, urn.NewUserSubject(f.userID), fresh.ID.UUID)
	require.NoError(t, err)
}

func TestRevokeSession_EnqueuesEndingTheWorkOSSession(t *testing.T) {
	t.Parallel()

	ctx, f := newSSOFixture(t)
	sessionID := "session_" + uuid.NewString()
	_, err := ssosignins.Record(ctx, f.ti.conn, f.signInParams(sessionID, time.Now()))
	require.NoError(t, err)

	out, err := ssosignins.RevokeSession(ctx, f.ti.conn, []string{f.orgID}, f.userID, sessionID)
	require.NoError(t, err)
	require.Equal(t, []string{f.orgID}, out.Organizations)
	require.Len(t, out.DeliveryIDs, 1)
	require.Equal(t, 1, pendingDeliveries(t, ctx, f))
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
	signIn, err := ssosignins.Record(ctx, f.ti.conn, f.signInParams(sessionID, time.Now()))
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
		enforced     bool
		impersonated bool
		revoke       ssosignins.RevokeReason
		wantLinked   bool
		wantRefused  bool
	}{
		{name: "own sign-in", enforced: true, impersonated: false, revoke: "", wantLinked: true, wantRefused: false},
		{name: "impersonated session", enforced: true, impersonated: true, revoke: "", wantLinked: false, wantRefused: false},
		{name: "enforcement off", enforced: false, impersonated: false, revoke: "", wantLinked: false, wantRefused: false},
		{name: "signed-out sign-in", enforced: true, impersonated: false, revoke: ssosignins.RevokeReasonSignOut, wantLinked: true, wantRefused: false},
		{name: "revoked sign-in", enforced: true, impersonated: false, revoke: ssosignins.RevokeReasonAdmin, wantLinked: false, wantRefused: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			if tc.enforced {
				flags := &feature.InMemory{}
				ssosignins.EnableForTests(flags, authCtx.ActiveOrganizationID)
				ti.sessionManager.SetFlags(flags)
			}
			workosSessionID := "session_" + uuid.NewString()
			current, err := ti.sessionManager.GetSession(ctx, *authCtx.SessionID)
			require.NoError(t, err)
			replacement := current
			replacement.WorkOSSessionID = workosSessionID
			if tc.impersonated {
				replacement.ImpersonatorEmail = "operator@example.com"
			}
			require.NoError(t, ti.sessionManager.UpdateSession(ctx, current, replacement))
			signIn, err := ssosignins.Record(ctx, ti.conn, ssosignins.RecordParams{OrganizationID: authCtx.ActiveOrganizationID, UserID: authCtx.UserID, WorkOSSessionID: workosSessionID, AuthenticationMethod: ssosignins.MethodSSO, Evidence: evidence(false, "", "", time.Now()), AuthenticatedAt: time.Now()})
			require.NoError(t, err)
			switch tc.revoke {
			case ssosignins.RevokeReasonSignOut:
				_, err = ssosignins.RevokeSession(ctx, ti.conn, []string{authCtx.ActiveOrganizationID}, authCtx.UserID, workosSessionID)
				require.NoError(t, err)
			case "":
			default:
				_, err = ssosignins.RevokeUser(ctx, ti.conn, []string{authCtx.ActiveOrganizationID}, authCtx.UserID, tc.revoke)
				require.NoError(t, err)
			}

			toolset := createIssuerGatedMintToolset(t, ctx, ti, "mint-sso-"+uuid.NewString()[:6])
			mintCtx := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPConnect, toolset.ID.String()))
			toolsetID := toolset.ID.String()
			minted, err := ti.service.MintUserSession(mintCtx, &sessionsgen.MintUserSessionPayload{ToolsetID: &toolsetID, McpServerID: nil, SessionToken: nil, ProjectSlugInput: nil})
			if tc.wantRefused {
				var oopsErr *oops.ShareableError
				require.ErrorAs(t, err, &oopsErr)
				require.Equal(t, oops.CodeUnauthorized, oopsErr.Code, "a revoked sign-in never mints an unlinked grant")
				return
			}
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

func TestMintUserSession_FailsClosedWhenTheSignInLookupFails(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enforceSSOSignIns(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	current, err := ti.sessionManager.GetSession(ctx, *authCtx.SessionID)
	require.NoError(t, err)
	require.NoError(t, ti.sessionManager.ClearSession(ctx, current))

	toolset := createIssuerGatedMintToolset(t, ctx, ti, "mint-sso-"+uuid.NewString()[:6])
	mintCtx := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPConnect, toolset.ID.String()))
	toolsetID := toolset.ID.String()
	_, err = ti.service.MintUserSession(mintCtx, &sessionsgen.MintUserSessionPayload{ToolsetID: &toolsetID, McpServerID: nil, SessionToken: nil, ProjectSlugInput: nil})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnavailable, oopsErr.Code, "no unlinked grant while enforcement is on")
}

func TestRecord_ASessionAuthenticatedBeforeARevocationStaysFencedWhenReused(t *testing.T) {
	t.Parallel()

	ctx, f := newSSOFixture(t)
	authenticated := time.Now().Add(-time.Hour)
	_, err := ssosignins.RevokeUser(ctx, f.ti.conn, []string{f.orgID}, f.userID, ssosignins.RevokeReasonSignOutEverywhere)
	require.NoError(t, err)

	// A WorkOS session never recorded here, authenticated before the
	// revocation and handed back by AuthKit afterwards.
	workosSessionID := "session_" + uuid.NewString()
	reused := f.signInParams(workosSessionID, time.Now())
	reused.Evidence.IDPAuthenticatedAt = authenticated
	recorded, err := ssosignins.Record(ctx, f.ti.conn, reused)
	require.NoError(t, err)
	require.True(t, recorded.Revoked, "the real authentication predates the cutoff")

	var ended []string
	for _, d := range deliveries(t, ctx, f) {
		if d.Kind == string(ssosignins.DeliveryWorkOSSession) && d.DeliveredAt.Time.IsZero() {
			ended = append(ended, d.Target)
		}
	}
	require.Contains(t, ended, workosSessionID, "a sign-in recorded revoked enqueues ending its WorkOS session")
}

func TestRecord_InheritsOnlySessionWideRevocations(t *testing.T) {
	t.Parallel()

	ctx, f := newSSOFixture(t)
	otherOrg := seedOtherOrganization(t, ctx, f.ti.conn)
	workosSessionID := "session_" + uuid.NewString()
	elsewhere := f.signInParams(workosSessionID, time.Now())
	elsewhere.OrganizationID = otherOrg
	_, err := ssosignins.Record(ctx, f.ti.conn, elsewhere)
	require.NoError(t, err)
	_, err = ssosignins.RevokeUser(ctx, f.ti.conn, []string{otherOrg}, f.userID, ssosignins.RevokeReasonAdmin)
	require.NoError(t, err)

	here, err := ssosignins.Record(ctx, f.ti.conn, f.signInParams(workosSessionID, time.Now()))
	require.NoError(t, err)
	require.False(t, here.Revoked, "an administrator's revocation stays in its organization")
}

func TestRevokeSession_WithNothingRecordedStillEndsTheWorkOSSession(t *testing.T) {
	t.Parallel()

	ctx, f := newSSOFixture(t)
	out, err := ssosignins.RevokeSession(ctx, f.ti.conn, []string{f.orgID}, f.userID, "session_"+uuid.NewString())
	require.NoError(t, err)
	require.Len(t, out.DeliveryIDs, 1, "the signed-out WorkOS session is ended durably")
	require.Equal(t, []string{f.orgID}, out.Organizations)
}

type goneRevoker struct{ recordingRevoker }

func (g *goneRevoker) RevokeSession(_ context.Context, id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, id)
	return &workos.APIError{Method: "POST", Path: "/user_management/sessions/revoke", StatusCode: http.StatusNotFound, Body: "not found"}
}

func TestDeliveries_AWorkOSSessionThatIsGoneCountsAsDelivered(t *testing.T) {
	t.Parallel()

	ctx, f := newSSOFixture(t)
	out, err := ssosignins.RevokeSession(ctx, f.ti.conn, []string{f.orgID}, f.userID, "session_"+uuid.NewString())
	require.NoError(t, err)

	gone := &goneRevoker{recordingRevoker: recordingRevoker{mu: sync.Mutex{}, calls: nil, fail: false, ctxErrs: nil}}
	deliverer := ssosignins.NewDeliverer(testenv.NewLogger(t), testenv.NewMeterProvider(t), f.ti.conn, gone, gone, nil)
	deliverer.DeliverNow(ctx, out.Organizations, out.DeliveryIDs)
	deliverer.Wait()
	require.Len(t, gone.calls, 1)
	require.Equal(t, 0, pendingDeliveries(t, ctx, f))
}
