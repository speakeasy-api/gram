package remotesessions_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	orgclientsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
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
	_, err = ti.conn.Exec(ctx, `UPDATE remote_session_issuers SET organization_id=$2 WHERE id=$1`, platformID, authCtx.ActiveOrganizationID)
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
	_, err := ti.conn.Exec(ctx, `UPDATE remote_session_clients SET scope=ARRAY['openid','email'] WHERE id=$1`, clientID)
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
		_, err = ti.conn.Exec(ctx, `INSERT INTO users(id,email,display_name) VALUES($1,$2,'Test user')`, userID, userID+"@example.test")
		require.NoError(t, err)
		_, err = ti.conn.Exec(ctx, `INSERT INTO organization_user_relationships(organization_id,user_id) VALUES($1,$2)`, org, userID)
		require.NoError(t, err)
		config := hash
		if i == 2 {
			config = "old-config"
		}
		observed := time.Now().UTC().Add(-time.Duration(age) * 24 * time.Hour)
		_, err = ti.conn.Exec(ctx, `INSERT INTO trusted_issuer_sessions(organization_id,remote_session_client_id,subject_urn,credential_config_hash,observation_status,observed_at,credential_obtained_at,last_refresh_succeeded_at,refresh_token_encrypted) VALUES($1,$2,$3,$4,'durable_credential_present',$5,$6,$7,'must-not-decrypt')`, org, clientID, "user:"+userID, config, observed, observed.Add(-time.Hour), observed)
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
	wire, err := json.Marshal(got)
	require.NoError(t, err)
	require.NotContains(t, string(wire), "must-not-decrypt")
	require.NotContains(t, string(wire), "user:")
	_, err = ti.conn.Exec(ctx, `UPDATE remote_session_clients SET scope=ARRAY['openid','email','offline_access'] WHERE id=$1`, clientID)
	require.NoError(t, err)
	got, err = ti.service.GetClientDelegationStatus(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "unknown", got.Status)
	require.Empty(t, got.Observations)
}
