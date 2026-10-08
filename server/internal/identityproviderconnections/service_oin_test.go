package identityproviderconnections_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/identity_provider_connections"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/provisiontest"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	oinSecret      = "oin-secret-7f3a9c2e1b4d"
	oinOtherSecret = "oin-secret-rotated-5e8d0a"
)

func createOINConnection(t *testing.T, ctx context.Context, si *serviceInstance) *gen.OktaIdentityProviderConnection {
	t.Helper()

	created, err := si.svc.Create(ctx, &gen.CreatePayload{SessionToken: nil, OrgURL: fullOrgURL, ListingMode: new(identityproviderconnections.ListingModeOIN)})
	require.NoError(t, err)
	return created
}

func submitOIN(ctx context.Context, si *serviceInstance, id, secret string) (*gen.OktaIdentityProviderConnection, error) {
	submitted, err := si.svc.SubmitClientID(ctx, &gen.SubmitClientIDPayload{SessionToken: nil, ID: id, ClientID: testClientID, ClientSecret: &secret})
	if err != nil {
		return nil, fmt.Errorf("submit oin client id: %w", err)
	}
	return submitted, nil
}

// verifiedOINConnection submits oinSecret, which the fake then requires.
func verifiedOINConnection(t *testing.T, ctx context.Context, si *serviceInstance) *gen.OktaIdentityProviderConnection {
	t.Helper()

	si.oktaFakes.Fake(fullOrgURL).RequireClientSecret(oinSecret, si.enc)
	created := createOINConnection(t, ctx, si)
	submitted, err := submitOIN(ctx, si, created.ID, oinSecret)
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusVerified, submitted.Status)
	return submitted
}

func storedOINSecret(t *testing.T, ctx context.Context, si *serviceInstance, id string) string {
	t.Helper()

	managed, err := si.provisioner.GetManagedClient(ctx, si.orgID, mustParseUUID(t, id))
	require.NoError(t, err)
	if managed.ClientSecretEncrypted == "" {
		return ""
	}
	plaintext, err := si.enc.Decrypt(managed.ClientSecretEncrypted)
	require.NoError(t, err)
	return plaintext
}

// requireNoSecretLeak checks the views and every audit row of the organization for the plaintext secrets.
func requireNoSecretLeak(t *testing.T, ctx context.Context, si *serviceInstance, secrets []string, views ...*gen.OktaIdentityProviderConnection) {
	t.Helper()

	var surfaces strings.Builder
	for _, view := range views {
		encoded, err := json.Marshal(view) //nolint:musttag // serialized only to search the values for a substring; key names are irrelevant
		require.NoError(t, err)
		surfaces.Write(encoded)
	}
	rows, err := auditrepo.New(si.conn.conn).ListAuditLogs(ctx, auditrepo.ListAuditLogsParams{
		OrganizationID:         si.orgID,
		IncludeAssistantEvents: true,
	})
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	for _, row := range rows {
		for _, text := range []string{row.ActorDisplayName.String, row.SubjectID, row.SubjectDisplayName.String, row.SubjectSlug.String} {
			surfaces.WriteString(text)
		}
		surfaces.Write(row.BeforeSnapshot)
		surfaces.Write(row.AfterSnapshot)
		surfaces.Write(row.Metadata)
	}
	for _, secret := range secrets {
		require.NotContains(t, surfaces.String(), secret)
	}
}

func TestCreate_OINProvisionsSecretClient(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createOINConnection(t, ctx, si)
	require.Equal(t, identityproviderconnections.ListingModeOIN, created.ListingMode)
	require.Nil(t, created.JwksURL)
	require.Nil(t, created.ActiveKey)
	require.False(t, created.DpopRequired)

	keys := checklistKeys(created.Checklist)
	require.Contains(t, keys, identityproviderconnections.ChecklistKeyAddOINApp)
	require.NotContains(t, keys, identityproviderconnections.ChecklistKeyPublicKeyAuth)
	require.NotContains(t, keys, identityproviderconnections.ChecklistKeyDPoP)

	managed, err := si.provisioner.GetManagedClient(ctx, si.orgID, mustParseUUID(t, created.ID))
	require.NoError(t, err)
	require.Equal(t, remotesessions.TokenEndpointAuthMethodBasic, managed.AuthMethod)
	require.False(t, managed.JSONWebKeySetID.Valid)
	require.Empty(t, managed.ClientSecretEncrypted)
}

func TestCreate_OINRequiresClientSecretBasicDiscovery(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	si.discovery.authMethods = []string{"private_key_jwt"}
	_, err := si.svc.Create(ctx, &gen.CreatePayload{SessionToken: nil, OrgURL: fullOrgURL, ListingMode: new(identityproviderconnections.ListingModeOIN)})
	requireOopsCode(t, err, oops.CodeFailedPrecondition)
}

func TestSubmitClientID_OINRequiresSecret(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createOINConnection(t, ctx, si)
	_, err := si.svc.SubmitClientID(ctx, &gen.SubmitClientIDPayload{SessionToken: nil, ID: created.ID, ClientID: testClientID, ClientSecret: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)

	for _, secret := range []string{"", "   ", "has inner space", strings.Repeat("s", 513)} {
		_, err := submitOIN(ctx, si, created.ID, secret)
		requireOopsCode(t, err, oops.CodeBadRequest)
	}

	fetched, err := si.svc.Get(ctx, &gen.GetPayload{SessionToken: nil, ID: &created.ID})
	require.NoError(t, err)
	require.False(t, fetched.Connection.ClientIDSubmitted)
}

func TestSubmitClientID_CustomAppRefusesSecret(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	_, err := submitOIN(ctx, si, created.ID, oinSecret)
	requireOopsCode(t, err, oops.CodeBadRequest)

	fetched, err := si.svc.Get(ctx, &gen.GetPayload{SessionToken: nil, ID: &created.ID})
	require.NoError(t, err)
	require.False(t, fetched.Connection.ClientIDSubmitted)
	requireNoSecretLeak(t, ctx, si, []string{oinSecret}, fetched.Connection)
}

func TestSubmitClientID_OINWrongSecretIsNotPersisted(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	si.oktaFakes.Fake(fullOrgURL).RequireClientSecret(oinSecret, si.enc)
	created := createOINConnection(t, ctx, si)
	_, err := submitOIN(ctx, si, created.ID, oinOtherSecret)
	requireOopsCode(t, err, oops.CodeFailedPrecondition)

	fetched, err := si.svc.Get(ctx, &gen.GetPayload{SessionToken: nil, ID: &created.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusPending, fetched.Connection.Status)
	require.False(t, fetched.Connection.ClientIDSubmitted)
	require.Equal(t, []string{identityproviderconnections.ReasonSecretRejected}, fetched.Connection.VerificationReasons)
	require.Empty(t, storedOINSecret(t, ctx, si, created.ID), "a rejected secret is never stored")
	requireNoSecretLeak(t, ctx, si, []string{oinSecret, oinOtherSecret}, created, fetched.Connection)
}

func TestSubmitClientID_OINVerifiesWithBearerToken(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	fake := si.oktaFakes.Fake(fullOrgURL)
	fake.RequireClientSecret(oinSecret, si.enc)
	fake.SetBearerOnly(true)
	created := createOINConnection(t, ctx, si)

	// Surrounding whitespace from a paste is trimmed before storage.
	submitted, err := submitOIN(ctx, si, created.ID, " "+oinSecret+"\n")
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusVerified, submitted.Status)
	require.True(t, submitted.ClientIDSubmitted)
	require.False(t, submitted.DpopRequired)
	require.Empty(t, submitted.VerificationReasons, "a Bearer token is not a dpop_not_bound finding for client-secret connections")
	require.Nil(t, submitted.LastError)
	require.Equal(t, remotesessions.TokenEndpointAuthMethodBasic, fake.LastAuthMethod())
	require.Equal(t, oinSecret, storedOINSecret(t, ctx, si, created.ID))
	requireNoSecretLeak(t, ctx, si, []string{oinSecret}, created, submitted)
}

func TestReplaceClientSecret_OINVerifiesNewSecret(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	verified := verifiedOINConnection(t, ctx, si)

	si.oktaFakes.Fake(fullOrgURL).RequireClientSecret(oinOtherSecret, si.enc)
	replaced, err := si.svc.ReplaceClientSecret(ctx, &gen.ReplaceClientSecretPayload{SessionToken: nil, ID: verified.ID, ClientSecret: oinOtherSecret})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusVerified, replaced.Status)
	require.Equal(t, oinOtherSecret, storedOINSecret(t, ctx, si, verified.ID))

	replaces, err := audittest.AuditLogCountByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionReplaceClientSecret)
	require.NoError(t, err)
	require.EqualValues(t, 1, replaces)
	record, err := audittest.LatestAuditLogByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionReplaceClientSecret)
	require.NoError(t, err)
	require.Equal(t, si.authCtx.UserID, record.ActorID)
	requireNoSecretLeak(t, ctx, si, []string{oinSecret, oinOtherSecret}, verified, replaced)
}

func TestReplaceClientSecret_RejectedKeepsPreviousSecret(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	verified := verifiedOINConnection(t, ctx, si)

	_, err := si.svc.ReplaceClientSecret(ctx, &gen.ReplaceClientSecretPayload{SessionToken: nil, ID: verified.ID, ClientSecret: oinOtherSecret})
	requireOopsCode(t, err, oops.CodeFailedPrecondition)

	fetched, err := si.svc.Get(ctx, &gen.GetPayload{SessionToken: nil, ID: &verified.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusVerified, fetched.Connection.Status)
	require.Nil(t, fetched.Connection.LastError, "a rejected replacement records no failure")
	require.Equal(t, oinSecret, storedOINSecret(t, ctx, si, verified.ID))

	reverified, err := si.svc.Verify(ctx, &gen.VerifyPayload{SessionToken: nil, ID: verified.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusVerified, reverified.Status, "the previous secret still works")

	replaces, err := audittest.AuditLogCountByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionReplaceClientSecret)
	require.NoError(t, err)
	require.EqualValues(t, 0, replaces, "a refused attempt changes nothing and is not audited")
	requireNoSecretLeak(t, ctx, si, []string{oinSecret, oinOtherSecret}, verified, fetched.Connection, reverified)
}

func TestReplaceClientSecret_RefusesCustomApp(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	submitClientID(t, ctx, si, created.ID)
	_, err := si.svc.ReplaceClientSecret(ctx, &gen.ReplaceClientSecretPayload{SessionToken: nil, ID: created.ID, ClientSecret: oinSecret})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestReplaceClientSecret_RefusesPending(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createOINConnection(t, ctx, si)
	_, err := si.svc.ReplaceClientSecret(ctx, &gen.ReplaceClientSecretPayload{SessionToken: nil, ID: created.ID, ClientSecret: oinSecret})
	requireOopsCode(t, err, oops.CodeConflict)
	require.Empty(t, storedOINSecret(t, ctx, si, created.ID))
}

func TestVerify_OINSecretRejectedRecordsReason(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	verified := verifiedOINConnection(t, ctx, si)

	// The secret was rotated in Okta without updating Speakeasy.
	si.oktaFakes.Fake(fullOrgURL).RequireClientSecret(oinOtherSecret, si.enc)
	_, err := si.svc.Verify(ctx, &gen.VerifyPayload{SessionToken: nil, ID: verified.ID})
	requireOopsCode(t, err, oops.CodeFailedPrecondition)

	fetched, err := si.svc.Get(ctx, &gen.GetPayload{SessionToken: nil, ID: &verified.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusDegraded, fetched.Connection.Status)
	require.Equal(t, []string{identityproviderconnections.ReasonSecretRejected}, fetched.Connection.VerificationReasons)
	require.Equal(t, identityproviderconnections.LastErrorCredentialRejected, conv.PtrValOr(fetched.Connection.LastError, ""))
	requireNoSecretLeak(t, ctx, si, []string{oinSecret, oinOtherSecret}, fetched.Connection)
}

func TestRevoke_OINClearsSecret(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	verified := verifiedOINConnection(t, ctx, si)
	require.Equal(t, oinSecret, storedOINSecret(t, ctx, si, verified.ID))

	revoked, err := si.svc.Revoke(ctx, &gen.RevokePayload{SessionToken: nil, ID: verified.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusRevoked, revoked.Status)
	require.Empty(t, storedOINSecret(t, ctx, si, verified.ID))
	requireNoSecretLeak(t, ctx, si, []string{oinSecret}, verified, revoked)
}

func TestVerify_OINOnceDPoPBoundRefusesBearer(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	verified := verifiedOINConnection(t, ctx, si)
	require.True(t, verified.DpopRequired, "a DPoP-bound token pins the connection")

	si.oktaFakes.Fake(fullOrgURL).SetBearerOnly(true)
	reverified, err := si.svc.Verify(ctx, &gen.VerifyPayload{SessionToken: nil, ID: verified.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusDegraded, reverified.Status)
	require.Contains(t, reverified.VerificationReasons, identityproviderconnections.ReasonDPoPNotBound)
	require.True(t, reverified.DpopRequired, "the pin survives a Bearer-only verification")
}

// failingDecrypter cannot decrypt anything, as when the encryption key changed.
type failingDecrypter struct{}

func (failingDecrypter) Decrypt(string) (string, error) {
	return "", errors.New("cipher: message authentication failed")
}

func TestVerify_OINUndecryptableSecretIsNotUnreachable(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	verified := verifiedOINConnection(t, ctx, si)

	si.oktaFakes.Fake(fullOrgURL).RequireClientSecret(oinSecret, failingDecrypter{})
	_, err := si.svc.Verify(ctx, &gen.VerifyPayload{SessionToken: nil, ID: verified.ID})
	requireOopsCode(t, err, oops.CodeUnexpected)
	require.ErrorContains(t, err, "the stored client secret could not be read; replace it")
}

func TestCreate_AbandonsOINOrphanWithoutKeySet(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	orphanID := provisiontest.CreateConnection(t, ctx, si.conn.conn, si.orgID, identityproviderconnections.ProviderOkta)
	issuerID := createIssuer(t, ctx, si.conn.conn, si.orgID, noProject, tokenEndpoint)
	managed, err := si.provisioner.ProvisionClient(ctx, secretParams(si.orgID, orphanID, issuerID))
	require.NoError(t, err)
	require.False(t, managed.JSONWebKeySetID.Valid)
	secret, err := si.enc.Encrypt([]byte(oinSecret))
	require.NoError(t, err)
	dbtx := testenv.BeginTx(t, ctx, si.conn.conn)
	_, err = si.provisioner.SetClientID(ctx, dbtx, setClientIDParams(si.orgID, orphanID, secret))
	require.NoError(t, err)
	require.NoError(t, dbtx.Commit(ctx))

	created := createOINConnection(t, ctx, si)
	require.NotEqual(t, orphanID.String(), created.ID)

	orphan, err := repo.New(si.conn.conn).GetIdentityProviderConnectionIncludingDeleted(ctx, repo.GetIdentityProviderConnectionIncludingDeletedParams{ID: orphanID, OrganizationID: si.orgID})
	require.NoError(t, err)
	require.True(t, orphan.Deleted, "the orphan was tombstoned")
	_, err = si.provisioner.GetManagedClient(ctx, si.orgID, orphanID)
	require.ErrorIs(t, err, identityproviderconnections.ErrNotProvisioned, "the orphan's client was tombstoned")
}
