package usersessions_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// Recording an access-token revocation ends only an access-only session of
// the serving issuer whose tenant is the issuer's owner: one that records the
// organization, or, written before sessions recorded one, whose project is
// in it. Organization-tier sessions, with no project, are included.
func TestRevokeAccessOnlyUserSession_ScopesToTheIssuersOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	org := authCtx.ActiveOrganizationID
	issuer := seedIssuer(t, ctx, ti, "revoke-access-only")
	orgTier, err := testrepo.New(ti.conn).InsertOrganizationTierUserSessionIssuerFixture(ctx, testrepo.InsertOrganizationTierUserSessionIssuerFixtureParams{
		OrganizationID:     pgtype.Text{String: org, Valid: true},
		Slug:               "revoke-access-only-org-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)
	none := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	session := func(issuerID uuid.UUID, refreshable bool) uuid.UUID {
		t.Helper()
		return seedTrendRow(t, ctx, ti, issuerID, trendRow{identity: "", selection: nil, refreshable: refreshable, credential: none, selfRoot: false, created: time.Now().Add(-time.Minute), deleted: nil, deadline: time.Now().Add(time.Hour), expires: time.Now().Add(time.Hour)})
	}
	q := repo.New(ti.conn)
	revoke := func(id, issuerID uuid.UUID, organization string) int64 {
		t.Helper()
		n, err := q.RevokeAccessOnlyUserSession(ctx, repo.RevokeAccessOnlyUserSessionParams{ID: id, UserSessionIssuerID: issuerID, OrganizationID: organization})
		require.NoError(t, err)
		return n
	}
	deleted := func(id, issuerID uuid.UUID) bool {
		t.Helper()
		at, err := q.GetUserSessionDeletedAtFixture(ctx, repo.GetUserSessionDeletedAtFixtureParams{ID: id, UserSessionIssuerID: issuerID})
		require.NoError(t, err)
		return at.Valid
	}
	other := "org_" + uuid.NewString()

	project := session(issuer, false)
	require.Zero(t, revoke(project, issuer, other), "another organization")
	require.Zero(t, revoke(project, orgTier, org), "another issuer")
	require.False(t, deleted(project, issuer))
	require.EqualValues(t, 1, revoke(project, issuer, org), "a project-tier session of the issuer's organization")

	legacy := session(issuer, false)
	n, err := q.ClearUserSessionOrganizationFixture(ctx, repo.ClearUserSessionOrganizationFixtureParams{ID: legacy, UserSessionIssuerID: issuer})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	require.Zero(t, revoke(legacy, issuer, other), "a legacy session whose project is in another organization's")
	require.EqualValues(t, 1, revoke(legacy, issuer, org), "a legacy session with no organization, in a project of this one")

	organizationTier := session(orgTier, false)
	require.Zero(t, revoke(organizationTier, orgTier, other))
	require.EqualValues(t, 1, revoke(organizationTier, orgTier, org), "an organization-tier session with no project")

	grant := session(issuer, true)
	require.Zero(t, revoke(grant, issuer, org), "a refreshable grant keeps its row")
	require.False(t, deleted(grant, issuer))
	require.Zero(t, revoke(project, issuer, org), "an ended session is not ended again")
}
