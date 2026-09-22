package externalcredentials_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	adminecgen "github.com/speakeasy-api/gram/server/gen/admin_external_credentials"
	gen "github.com/speakeasy-api/gram/server/gen/external_credentials"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	extkeysrepo "github.com/speakeasy-api/gram/server/internal/externalkeys/repo"
	ipcrepo "github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestDeleteGcpIamPlatformCredential_SoftDeletes(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	cred := createPlatformGCPAmbientCredential(t, ctx, ti, "platform-delete")

	err := ti.service.DeleteGcpIamPlatformCredential(withFreshAdmin(t, ctx, ti), &adminecgen.DeleteGcpIamPlatformCredentialPayload{
		ID:           cred.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	_, err = ti.service.GetGcpIamPlatformCredential(withAdmin(t, ctx), &adminecgen.GetGcpIamPlatformCredentialPayload{
		ID:           cred.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	result, err := ti.service.ListPlatformExternalCredentials(withAdmin(t, ctx), &adminecgen.ListPlatformExternalCredentialsPayload{
		Provider:     nil,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Empty(t, result.Credentials)
}

// A platform credential that managed signing keys still sign through cannot
// be deleted or re-pointed.
func TestGcpIamPlatformCredential_RefusesMutationWhileManagedKeysLive(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	cred := createPlatformGCPAmbientCredential(t, ctx, ti, "platform-managed")
	credID, err := uuid.Parse(cred.ID)
	require.NoError(t, err)

	conn, err := ipcrepo.New(ti.conn).CreateIdentityProviderConnection(ctx, ipcrepo.CreateIdentityProviderConnectionParams{OrganizationID: ti.orgID, Provider: "okta"})
	require.NoError(t, err)
	key, err := extkeysrepo.New(ti.conn).CreateExternalKey(ctx, extkeysrepo.CreateExternalKeyParams{
		OrganizationID:               conv.ToPGText(ti.orgID),
		ExternalCredentialID:         credID,
		Provider:                     "gcp_kms",
		Algorithm:                    "RS256",
		Name:                         "managed",
		CustomerGrantReference:       pgtype.Text{String: "", Valid: false},
		IdentityProviderConnectionID: conv.ToNullUUID(conn.ID),
	})
	require.NoError(t, err)

	err = ti.service.DeleteGcpIamPlatformCredential(withFreshAdmin(t, ctx, ti), &adminecgen.DeleteGcpIamPlatformCredentialPayload{ID: cred.ID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeConflict)

	_, err = ti.service.UpdateGcpIamPlatformCredential(withFreshAdmin(t, ctx, ti), &adminecgen.UpdateGcpIamPlatformCredentialPayload{
		ID: cred.ID, SessionToken: nil, Name: "renamed", ImpersonateServiceAccount: nil, WifPoolID: nil, WifProviderID: nil, WifProjectNumber: nil,
	})
	requireOopsCode(t, err, oops.CodeConflict)

	_, err = extkeysrepo.New(ti.conn).SoftDeleteExternalKey(ctx, extkeysrepo.SoftDeleteExternalKeyParams{ID: key.ID, OrganizationID: conv.ToPGText(ti.orgID), Provider: "gcp_kms"})
	require.NoError(t, err)
	require.NoError(t, ti.service.DeleteGcpIamPlatformCredential(withFreshAdmin(t, ctx, ti), &adminecgen.DeleteGcpIamPlatformCredentialPayload{ID: cred.ID, SessionToken: nil}))
}

// Deleting a missing id is an idempotent no-op.
func TestDeleteGcpIamPlatformCredential_MissingIsNoOp(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	err := ti.service.DeleteGcpIamPlatformCredential(withFreshAdmin(t, ctx, ti), &adminecgen.DeleteGcpIamPlatformCredentialPayload{
		ID:           uuid.NewString(),
		SessionToken: nil,
	})
	require.NoError(t, err)
}

// The platform delete does not reach organization-scoped rows.
func TestDeleteGcpIamPlatformCredential_ExcludesOrgCredential(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	orgCred := createGCPImpersonationCredential(t, ctx, ti, "org-scoped")

	err := ti.service.DeleteGcpIamPlatformCredential(withFreshAdmin(t, ctx, ti), &adminecgen.DeleteGcpIamPlatformCredentialPayload{
		ID:           orgCred.ID,
		SessionToken: nil,
	})
	require.NoError(t, err, "wrong-scope delete is a no-op")

	got, err := ti.service.GetGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.GetGcpIamCredentialPayload{
		ID:           orgCred.ID,
		SessionToken: nil,
	})
	require.NoError(t, err, "the org credential must survive a platform delete")
	require.Equal(t, orgCred.ID, got.ID)
}
