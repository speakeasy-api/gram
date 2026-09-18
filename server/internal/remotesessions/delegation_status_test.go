package remotesessions_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	orgclientshttp "github.com/speakeasy-api/gram/server/gen/http/organization_remote_session_clients/server"
	orgclientsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

func TestGetClientDelegationStatusUnauthorized(t *testing.T) {
	t.Parallel()
	_, ti := newTestService(t)
	_, err := ti.service.GetClientDelegationStatus(context.Background(), &orgclientsgen.GetClientDelegationStatusPayload{ID: uuid.NewString()})
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestGetClientDelegationStatusRequiresOrganizationAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})
	_, err := ti.service.GetClientDelegationStatus(ctx, &orgclientsgen.GetClientDelegationStatusPayload{ID: uuid.NewString()})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestGetClientDelegationStatusUnknownClient(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	_, err := ti.service.GetClientDelegationStatus(ctx, &orgclientsgen.GetClientDelegationStatusPayload{ID: uuid.NewString()})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetClientDelegationStatusUnknownAndTenantIsolated(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	platformID := seedGlobalRemoteIssuer(t, ctx, ti.conn, "delegation-status-platform")
	own, err := ti.service.CreateClient(ctx, newCreateClientPayload(platformID.String(), nil, nil))
	require.NoError(t, err)
	status, err := ti.service.GetClientDelegationStatus(ctx, &orgclientsgen.GetClientDelegationStatusPayload{ID: own.ID})
	require.NoError(t, err)
	require.Equal(t, "unknown", status.Status)
	require.Empty(t, status.Observations)
	require.NotEmpty(t, status.WindowStart)
	otherOrg := createOrganization(t, ctx, ti.conn, "delegation-status-other")
	otherClient := seedOrgLevelRemoteClient(t, ctx, ti.conn, otherOrg, platformID, "other-client")
	_, err = ti.service.GetClientDelegationStatus(ctx, &orgclientsgen.GetClientDelegationStatusPayload{ID: otherClient.String()})
	requireOopsCode(t, err, oops.CodeNotFound)
	// Generic client lookup also accepts an issuer owned by this organization;
	// retained delegation data must still require ownership of the client.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	err = repo.New(ti.conn).SetRemoteSessionIssuerOrganizationFixture(ctx, repo.SetRemoteSessionIssuerOrganizationFixtureParams{ID: platformID, OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID)})
	require.NoError(t, err)
	_, err = ti.service.GetClientDelegationStatus(ctx, &orgclientsgen.GetClientDelegationStatusPayload{ID: otherClient.String()})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetClientDelegationStatusCurrentObservationsOnly(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	org := authCtx.ActiveOrganizationID
	issuerID := seedGlobalRemoteIssuer(t, ctx, ti.conn, "delegation-observations")
	clientID := seedOrgLevelRemoteClient(t, ctx, ti.conn, org, issuerID, "delegation-observations")
	_, err := repo.New(ti.conn).UpdateOrganizationRemoteSessionClient(ctx, repo.UpdateOrganizationRemoteSessionClientParams{ID: clientID, OrganizationID: conv.ToPGText(org), Scope: []string{"openid", "email"}})
	require.NoError(t, err)
	createTrustedClientOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "delegation-observations", issuerID, clientID)
	q := repo.New(ti.conn)
	row, err := q.GetOrganizationRemoteSessionClientByID(ctx, repo.GetOrganizationRemoteSessionClientByIDParams{ID: clientID, OrganizationID: conv.ToPGText(org)})
	require.NoError(t, err)
	issuer, err := q.GetOrganizationRemoteSessionIssuerByID(ctx, repo.GetOrganizationRemoteSessionIssuerByIDParams{ID: issuerID, OrganizationID: conv.ToPGText(org), IncludeGlobal: true})
	require.NoError(t, err)
	hash := remotesessions.FederatedDelegationConfigurationHash(org, issuer, row.RemoteSessionClient)
	require.NotEmpty(t, hash)
	// Invalid ciphertext intentionally proves the status read never decrypts.
	for i, age := range []int{1, 31, 1} {
		userID := uuid.NewString()
		err = testrepo.New(ti.conn).InsertUserFixture(ctx, testrepo.InsertUserFixtureParams{ID: userID, Email: userID + "@example.test", DisplayName: "Test user"})
		require.NoError(t, err)
		err = testrepo.New(ti.conn).CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{OrganizationID: org, UserID: conv.ToPGText(userID)})
		require.NoError(t, err)
		config := hash
		if i == 2 {
			config = "old-config"
		}
		observed := time.Now().UTC().Add(-time.Duration(age) * 24 * time.Hour)
		err = q.InsertTrustedDelegationObservationFixture(ctx, repo.InsertTrustedDelegationObservationFixtureParams{OrganizationID: conv.ToPGText(org), ClientID: uuid.NullUUID{UUID: clientID, Valid: true}, SubjectUrn: "user:" + userID, ConfigHash: conv.ToPGText(config), ObservedAt: delegationTestTimestamp(observed), ObtainedAt: delegationTestTimestamp(observed.Add(-time.Hour)), RefreshedAt: delegationTestTimestamp(observed)})
		require.NoError(t, err)
	}
	payload := &orgclientsgen.GetClientDelegationStatusPayload{ID: clientID.String()}
	got, err := ti.service.GetClientDelegationStatus(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "observed", got.Status)
	require.Len(t, got.Observations, 1)
	require.Equal(t, int64(1), got.Observations[0].Count)
	require.Equal(t, "durable_credential_present", got.Observations[0].Status)
	require.NotNil(t, got.Observations[0].LastObservedAt)
	require.NotNil(t, got.Observations[0].LastCredentialObtainedAt)
	require.NotNil(t, got.Observations[0].LastRefreshSucceededAt)
	require.NotEqual(t, *got.Observations[0].LastCredentialObtainedAt, *got.Observations[0].LastRefreshSucceededAt)
	wire, err := json.Marshal(orgclientshttp.NewGetClientDelegationStatusResponseBody(got))
	require.NoError(t, err)
	require.NotContains(t, string(wire), "must-not-decrypt")
	require.NotContains(t, string(wire), "user:")
	_, err = repo.New(ti.conn).UpdateOrganizationRemoteSessionClient(ctx, repo.UpdateOrganizationRemoteSessionClientParams{ID: clientID, OrganizationID: conv.ToPGText(org), Scope: []string{"openid", "email", "offline_access"}})
	require.NoError(t, err)
	got, err = ti.service.GetClientDelegationStatus(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "unknown", got.Status)
	require.Empty(t, got.Observations)
}

func delegationTestTimestamp(v time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: v, Valid: true, InfinityModifier: pgtype.Finite}
}
