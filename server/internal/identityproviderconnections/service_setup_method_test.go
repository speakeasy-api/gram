package identityproviderconnections_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/identity_provider_connections"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections"
	jwksrepo "github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func setSetupMethod(ctx context.Context, si *serviceInstance, id, listingMode string) (*gen.OktaIdentityProviderConnection, error) {
	return si.svc.SetSetupMethod(ctx, &gen.SetSetupMethodPayload{SessionToken: nil, ID: id, ListingMode: listingMode}) //nolint:wrapcheck // tests assert the service's oops errors
}

func hasAuditAction(t *testing.T, ctx context.Context, si *serviceInstance, action audit.Action) bool {
	t.Helper()

	return slices.ContainsFunc(auditActions(t, ctx, si.conn.conn, si.orgID), func(entry [2]string) bool {
		return entry[1] == string(action)
	})
}

func requireKeySetLive(t *testing.T, ctx context.Context, si *serviceInstance, setID uuid.UUID, live bool) {
	t.Helper()

	_, err := jwksrepo.New(si.conn.conn).GetJsonWebKeySet(ctx, jwksrepo.GetJsonWebKeySetParams{ID: setID, OrganizationID: si.orgID})
	if live {
		require.NoError(t, err)
		return
	}
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestSetSetupMethod_OINToCustomAppProvisionsKeySet(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createOINConnection(t, ctx, si)
	switched, err := setSetupMethod(ctx, si, created.ID, identityproviderconnections.ListingModeCustomApp)
	require.NoError(t, err)
	require.Equal(t, created.ID, switched.ID)
	require.Equal(t, identityproviderconnections.ListingModeCustomApp, switched.ListingMode)
	require.NotNil(t, switched.JwksURL)
	require.NotNil(t, switched.ActiveKey)
	require.Contains(t, checklistKeys(switched.Checklist), identityproviderconnections.ChecklistKeyPublicKeyAuth)

	managed, err := si.provisioner.GetManagedClient(ctx, si.orgID, mustParseUUID(t, created.ID))
	require.NoError(t, err)
	require.Equal(t, remotesessions.TokenEndpointAuthMethodPrivateKeyJWT, managed.AuthMethod)
	require.True(t, managed.JSONWebKeySetID.Valid)
	require.Contains(t, managedJWKS(t, ctx, si, managed), managed.ActiveKid)
	require.True(t, hasAuditAction(t, ctx, si, audit.ActionIdentityProviderConnectionSetSetupMethod))

	submitted := submitClientID(t, ctx, si, created.ID)
	require.Equal(t, identityproviderconnections.StatusVerified, submitted.Status)
}

func TestSetSetupMethod_CustomAppToOINRetiresKeySet(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	before, err := si.provisioner.GetManagedClient(ctx, si.orgID, mustParseUUID(t, created.ID))
	require.NoError(t, err)

	switched, err := setSetupMethod(ctx, si, created.ID, identityproviderconnections.ListingModeOIN)
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.ListingModeOIN, switched.ListingMode)
	require.Nil(t, switched.JwksURL)
	require.Nil(t, switched.ActiveKey)
	require.Contains(t, checklistKeys(switched.Checklist), identityproviderconnections.ChecklistKeyAddOINApp)

	managed, err := si.provisioner.GetManagedClient(ctx, si.orgID, mustParseUUID(t, created.ID))
	require.NoError(t, err)
	require.Equal(t, before.ClientRowID, managed.ClientRowID)
	require.Equal(t, remotesessions.TokenEndpointAuthMethodBasic, managed.AuthMethod)
	require.False(t, managed.JSONWebKeySetID.Valid)
	_, err = remotesessionsrepo.New(si.conn.conn).GetRemoteSessionClientJsonWebKeySetDocument(ctx, before.ClientRowID)
	require.ErrorIs(t, err, pgx.ErrNoRows, "the client no longer serves a key set")
	requireKeySetLive(t, ctx, si, before.JSONWebKeySetID.UUID, true)

	si.oktaFakes.Fake(fullOrgURL).RequireClientSecret(oinSecret, si.enc)
	submitted, err := submitOIN(ctx, si, created.ID, oinSecret)
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusVerified, submitted.Status)
	requireKeySetLive(t, ctx, si, before.JSONWebKeySetID.UUID, false)
}

func TestSetSetupMethod_SwitchingBackReattachesTheParkedKey(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createOINConnection(t, ctx, si)
	first, err := setSetupMethod(ctx, si, created.ID, identityproviderconnections.ListingModeCustomApp)
	require.NoError(t, err)
	minted, err := si.provisioner.GetManagedClient(ctx, si.orgID, mustParseUUID(t, created.ID))
	require.NoError(t, err)

	for range 3 {
		_, err = setSetupMethod(ctx, si, created.ID, identityproviderconnections.ListingModeOIN)
		require.NoError(t, err)
		again, err := setSetupMethod(ctx, si, created.ID, identityproviderconnections.ListingModeCustomApp)
		require.NoError(t, err)
		require.Equal(t, first.JwksURL, again.JwksURL)
		require.Equal(t, first.ActiveKey, again.ActiveKey)
	}

	reattached, err := si.provisioner.GetManagedClient(ctx, si.orgID, mustParseUUID(t, created.ID))
	require.NoError(t, err)
	require.Equal(t, minted.JSONWebKeySetID, reattached.JSONWebKeySetID)
	require.Equal(t, minted.ActiveKid, reattached.ActiveKid)
	require.Contains(t, managedJWKS(t, ctx, si, reattached), minted.ActiveKid)

	submitted := submitClientID(t, ctx, si, created.ID)
	require.Equal(t, identityproviderconnections.StatusVerified, submitted.Status)
	requireKeySetLive(t, ctx, si, minted.JSONWebKeySetID.UUID, true)
}

func TestSetSetupMethod_RevokeRetiresParkedKeySet(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	before, err := si.provisioner.GetManagedClient(ctx, si.orgID, mustParseUUID(t, created.ID))
	require.NoError(t, err)
	_, err = setSetupMethod(ctx, si, created.ID, identityproviderconnections.ListingModeOIN)
	require.NoError(t, err)
	requireKeySetLive(t, ctx, si, before.JSONWebKeySetID.UUID, true)

	_, err = si.svc.Revoke(ctx, &gen.RevokePayload{SessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	requireKeySetLive(t, ctx, si, before.JSONWebKeySetID.UUID, false)
}

func TestSetSetupMethod_SwitchesBackAndForth(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	for _, mode := range []string{
		identityproviderconnections.ListingModeOIN,
		identityproviderconnections.ListingModeCustomApp,
		identityproviderconnections.ListingModeOIN,
		identityproviderconnections.ListingModeCustomApp,
	} {
		switched, err := setSetupMethod(ctx, si, created.ID, mode)
		require.NoError(t, err)
		require.Equal(t, mode, switched.ListingMode)
		require.Equal(t, identityproviderconnections.StatusPending, switched.Status)
	}

	submitted := submitClientID(t, ctx, si, created.ID)
	require.Equal(t, identityproviderconnections.StatusVerified, submitted.Status)
}

func TestSetSetupMethod_SameMethodIsNoOp(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createOINConnection(t, ctx, si)
	switched, err := setSetupMethod(ctx, si, created.ID, identityproviderconnections.ListingModeOIN)
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.ListingModeOIN, switched.ListingMode)
	require.False(t, hasAuditAction(t, ctx, si, audit.ActionIdentityProviderConnectionSetSetupMethod))
}

func TestSetSetupMethod_RefusedAfterClientIDSubmitted(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	submitClientID(t, ctx, si, created.ID)
	_, err := setSetupMethod(ctx, si, created.ID, identityproviderconnections.ListingModeOIN)
	requireOopsCode(t, err, oops.CodeConflict)

	managed, err := si.provisioner.GetManagedClient(ctx, si.orgID, mustParseUUID(t, created.ID))
	require.NoError(t, err)
	require.Equal(t, remotesessions.TokenEndpointAuthMethodPrivateKeyJWT, managed.AuthMethod)
}

func TestSetSetupMethod_RejectsUnknownMethod(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createOINConnection(t, ctx, si)
	_, err := setSetupMethod(ctx, si, created.ID, "saml")
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestSetSetupMethod_RequiresDiscoveredMethod(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	si.discovery.authMethods = []string{"private_key_jwt"}
	_, err := setSetupMethod(ctx, si, created.ID, identityproviderconnections.ListingModeOIN)
	requireOopsCode(t, err, oops.CodeFailedPrecondition)

	fetched, err := si.svc.Get(ctx, &gen.GetPayload{SessionToken: nil, ID: &created.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.ListingModeCustomApp, fetched.Connection.ListingMode)
}

func TestSetSetupMethod_OtherOrganizationCannotSwitch(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createOINConnection(t, ctx, si)
	otherCtx, _ := asOtherOrganization(t, ctx, si)
	_, err := setSetupMethod(otherCtx, si, created.ID, identityproviderconnections.ListingModeCustomApp)
	require.Error(t, err)

	managed, err := si.provisioner.GetManagedClient(ctx, si.orgID, mustParseUUID(t, created.ID))
	require.NoError(t, err)
	require.Equal(t, remotesessions.TokenEndpointAuthMethodBasic, managed.AuthMethod)
}

func TestSetSetupMethod_ConcurrentSwitchesAllSucceed(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createOINConnection(t, ctx, si)
	const switches = 4
	errs := make(chan error, switches)
	for range switches {
		go func() {
			_, err := setSetupMethod(ctx, si, created.ID, identityproviderconnections.ListingModeCustomApp)
			errs <- err
		}()
	}
	for range switches {
		require.NoError(t, <-errs)
	}

	managed, err := si.provisioner.GetManagedClient(ctx, si.orgID, mustParseUUID(t, created.ID))
	require.NoError(t, err)
	require.Equal(t, remotesessions.TokenEndpointAuthMethodPrivateKeyJWT, managed.AuthMethod)
	require.Contains(t, managedJWKS(t, ctx, si, managed), managed.ActiveKid)

	submitted := submitClientID(t, ctx, si, created.ID)
	require.Equal(t, identityproviderconnections.StatusVerified, submitted.Status)
}

func TestRetireParkedKeySets_LeavesAnAttachedSet(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	_, err := setSetupMethod(ctx, si, created.ID, identityproviderconnections.ListingModeOIN)
	require.NoError(t, err)
	_, err = setSetupMethod(ctx, si, created.ID, identityproviderconnections.ListingModeCustomApp)
	require.NoError(t, err)
	managed, err := si.provisioner.GetManagedClient(ctx, si.orgID, mustParseUUID(t, created.ID))
	require.NoError(t, err)

	retired, err := si.provisioner.RetireParkedKeySets(ctx, si.orgID, mustParseUUID(t, created.ID))
	require.NoError(t, err)
	require.Zero(t, retired)
	requireKeySetLive(t, ctx, si, managed.JSONWebKeySetID.UUID, true)
}
