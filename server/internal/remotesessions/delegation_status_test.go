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
	create := newCreateClientPayload(platformID.String(), nil, conv.PtrEmpty("test-secret"))
	create.Scope = []string{"openid", "email"}
	own, err := ti.service.CreateClient(ctx, create)
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
	affected, err := repo.New(ti.conn).SetRemoteSessionIssuerOrganizationFixture(ctx, repo.SetRemoteSessionIssuerOrganizationFixtureParams{ID: platformID, OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID)})
	require.NoError(t, err)
	require.Equal(t, int64(1), affected)
	affected, err = repo.New(ti.conn).SetRemoteSessionIssuerOrganizationFixture(ctx, repo.SetRemoteSessionIssuerOrganizationFixtureParams{ID: platformID, OrganizationID: conv.ToPGText(otherOrg)})
	require.NoError(t, err)
	require.Zero(t, affected, "cannot reassign another organization issuer")
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
	_, err := repo.New(ti.conn).UpdateOrganizationRemoteSessionClient(ctx, repo.UpdateOrganizationRemoteSessionClientParams{ID: clientID, OrganizationID: conv.ToPGText(org), Scope: []string{"openid", "email"}, ClientSecretEncrypted: conv.ToPGText("must-not-decrypt-client-secret")})
	require.NoError(t, err)
	createTrustedClientOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "delegation-observations", issuerID, clientID)
	q := repo.New(ti.conn)
	provider, err := newCIMDChallengeManager(t, ti, "https://gram.example.test").LoadFederatedDelegationProvider(ctx, org, issuerID, clientID)
	require.NoError(t, err)
	hash := provider.DelegationConfigurationHash()
	require.NotEmpty(t, hash)
	// Invalid ciphertext intentionally proves the status read never decrypts.
	now := time.Now().UTC().Truncate(time.Microsecond)
	for i, age := range []int{1, 31, 1, 2, 1, 1, 1} {
		userID := uuid.NewString()
		err = testrepo.New(ti.conn).InsertUserFixture(ctx, testrepo.InsertUserFixtureParams{ID: userID, Email: userID + "@example.test", DisplayName: "Test user"})
		require.NoError(t, err)
		err = testrepo.New(ti.conn).CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{OrganizationID: org, UserID: conv.ToPGText(userID)})
		require.NoError(t, err)
		config := hash
		if i == 2 {
			config = "old-config"
		}
		observed := now.Add(-time.Duration(age) * 24 * time.Hour)
		obtained, refreshed := observed.Add(-time.Hour), observed
		if i == 3 {
			// Different humans supply the maxima: catches selecting one row instead of max per column.
			obtained, refreshed = now.Add(-time.Hour), now.Add(-time.Minute)
		}
		status := "durable_credential_present"
		var expiry pgtype.Timestamptz
		switch i {
		case 4:
			expiry = delegationTestTimestamp(now.Add(-time.Hour))
		case 5:
			status = "refused"
		case 6:
			status = "configuration_failure"
		}
		_, err = q.UpsertTrustedDelegationCredential(ctx, repo.UpsertTrustedDelegationCredentialParams{
			OrganizationID: org, ClientID: clientID, IssuerID: issuerID, SubjectUrn: "user:" + userID,
			CredentialConfigHash: conv.ToPGText(config), ObservationStatus: conv.ToPGText(status),
			ObservedAt: delegationTestTimestamp(observed), CredentialObtainedAt: delegationTestTimestamp(obtained),
			LastRefreshSucceededAt: delegationTestTimestamp(refreshed), RefreshTokenEncrypted: conv.ToPGText("must-not-decrypt"), RefreshExpiresAt: expiry,
		})
		require.NoError(t, err)
	}
	payload := &orgclientsgen.GetClientDelegationStatusPayload{ID: clientID.String()}
	got, err := ti.service.GetClientDelegationStatus(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "observed", got.Status)
	require.Len(t, got.Observations, 4)
	counts := make(map[string]*orgclientsgen.DelegationStatusCount)
	for _, observation := range got.Observations {
		counts[observation.Status] = observation
	}
	durable := counts["durable_credential_present"]
	require.NotNil(t, durable)
	require.Equal(t, int64(2), durable.Count)
	require.Equal(t, now.Add(-24*time.Hour).Format(time.RFC3339Nano), *durable.LastObservedAt)
	require.Equal(t, now.Add(-time.Hour).Format(time.RFC3339Nano), *durable.LastCredentialObtainedAt)
	require.Equal(t, now.Add(-time.Minute).Format(time.RFC3339Nano), *durable.LastRefreshSucceededAt)
	for _, status := range []string{"reauthentication_required", "refused", "configuration_failure"} {
		require.NotNil(t, counts[status], status)
		require.Equal(t, int64(1), counts[status].Count, status)
	}
	wire, err := json.Marshal(orgclientshttp.NewGetClientDelegationStatusResponseBody(got))
	require.NoError(t, err)
	require.NotContains(t, string(wire), "must-not-decrypt")
	require.NotContains(t, string(wire), "user:")
	// Known expiration overrides retained observations without decrypting secrets.
	_, err = q.ForceRemoteSessionClientRegistrationFixture(ctx, repo.ForceRemoteSessionClientRegistrationFixtureParams{
		ID: clientID, ClientSecretExpiresAt: delegationTestTimestamp(now.Add(-time.Hour)),
	})
	require.NoError(t, err)
	got, err = ti.service.GetClientDelegationStatus(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "configuration_failure", got.Status)
	require.NotNil(t, got.Observations)
	require.Empty(t, got.Observations)
	_, err = q.ForceRemoteSessionClientRegistrationFixture(ctx, repo.ForceRemoteSessionClientRegistrationFixtureParams{ID: clientID})
	require.NoError(t, err)
	_, err = repo.New(ti.conn).UpdateOrganizationRemoteSessionClient(ctx, repo.UpdateOrganizationRemoteSessionClientParams{ID: clientID, OrganizationID: conv.ToPGText(org), Scope: []string{"openid", "email", "offline_access"}})
	require.NoError(t, err)
	got, err = ti.service.GetClientDelegationStatus(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "unknown", got.Status)
	require.Empty(t, got.Observations)
	// A private_key_jwt registration without a signing key is a configuration
	// failure, not a dependency error and not evidence of a current observation.
	_, err = q.UpdateOrganizationRemoteSessionClient(ctx, repo.UpdateOrganizationRemoteSessionClientParams{ID: clientID, OrganizationID: conv.ToPGText(org), TokenEndpointAuthMethod: conv.ToPGText("private_key_jwt")})
	require.NoError(t, err)
	got, err = ti.service.GetClientDelegationStatus(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "configuration_failure", got.Status)
	require.Empty(t, got.Observations)

}

func delegationTestTimestamp(v time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: v, Valid: true, InfinityModifier: pgtype.Finite}
}
