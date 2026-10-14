package externalcredentials_test

import (
	"strings"
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
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/provisiontest"
	ipcrepo "github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	jwksrepo "github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestDeleteGcpIamPlatformCredential_SoftDeletes(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	cred := createPlatformGCPAmbientCredential(t, ctx, ti, "platform-delete")

	err := ti.service.DeleteGcpIamPlatformCredential(withAdmin(t, ctx, ti), &adminecgen.DeleteGcpIamPlatformCredentialPayload{
		ID:           cred.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	_, err = ti.service.GetGcpIamPlatformCredential(withAdmin(t, ctx, ti), &adminecgen.GetGcpIamPlatformCredentialPayload{
		ID:           cred.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	result, err := ti.service.ListPlatformExternalCredentials(withAdmin(t, ctx, ti), &adminecgen.ListPlatformExternalCredentialsPayload{
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

	err = ti.service.DeleteGcpIamPlatformCredential(withAdmin(t, ctx, ti), &adminecgen.DeleteGcpIamPlatformCredentialPayload{ID: cred.ID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeConflict)

	_, err = ti.service.UpdateGcpIamPlatformCredential(withAdmin(t, ctx, ti), &adminecgen.UpdateGcpIamPlatformCredentialPayload{
		ID: cred.ID, SessionToken: nil, Name: "renamed", ImpersonateServiceAccount: nil, WifPoolID: nil, WifProviderID: nil, WifProjectNumber: nil,
	})
	requireOopsCode(t, err, oops.CodeConflict)

	_, err = extkeysrepo.New(ti.conn).SoftDeleteExternalKey(ctx, extkeysrepo.SoftDeleteExternalKeyParams{ID: key.ID, OrganizationID: conv.ToPGText(ti.orgID), Provider: "gcp_kms"})
	require.NoError(t, err)
	require.NoError(t, ti.service.DeleteGcpIamPlatformCredential(withAdmin(t, ctx, ti), &adminecgen.DeleteGcpIamPlatformCredentialPayload{ID: cred.ID, SessionToken: nil}))
}

// Issuer keys sign as the credential's account and their cleanup removes the
// grant from it: the account can't change while any issuer key holds a grant,
// and the credential can't be deleted while a live issuer key signs through it.
func TestGcpIamPlatformCredential_GuardsIssuerKeyGrants(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	signer := provisiontest.SigningServiceAccount()
	cred, err := ti.service.CreateGcpIamPlatformCredential(withAdmin(t, ctx, ti), &adminecgen.CreateGcpIamPlatformCredentialPayload{
		SessionToken: nil, Name: "platform-issuer", ImpersonateServiceAccount: conv.PtrEmpty(signer),
		WifPoolID: nil, WifProviderID: nil, WifProjectNumber: nil,
	})
	require.NoError(t, err)
	credID, err := uuid.Parse(cred.ID)
	require.NoError(t, err)
	backing, err := extkeysrepo.New(ti.conn).CreateExternalKey(ctx, extkeysrepo.CreateExternalKeyParams{
		OrganizationID:               conv.ToPGText(ti.orgID),
		ExternalCredentialID:         credID,
		Provider:                     "gcp_kms",
		Algorithm:                    "RS256",
		Name:                         "issuer",
		CustomerGrantReference:       pgtype.Text{String: "", Valid: false},
		IdentityProviderConnectionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	jq := jwksrepo.New(ti.conn)
	set, err := jq.CreateJsonWebKeySet(ctx, jwksrepo.CreateJsonWebKeySetParams{OrganizationID: ti.orgID, ExternalKeyID: backing.ID, Name: "issuer", IdentityProviderConnectionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}})
	require.NoError(t, err)
	_, err = jq.CreateInternalSessionIssuer(ctx, jwksrepo.CreateInternalSessionIssuerParams{OrganizationID: ti.orgID, JsonWebKeySetID: set.ID, Issuer: "https://app.example.com/oauth/idp/org/" + ti.orgID})
	require.NoError(t, err)
	key, err := jq.CreateJsonWebKey(ctx, jwksrepo.CreateJsonWebKeyParams{OrganizationID: ti.orgID, JsonWebKeySetID: set.ID, ExternalKeyID: backing.ID, State: "active", Kid: "kid-1", PublicJwk: []byte(`{"kty":"EC"}`)})
	require.NoError(t, err)

	update := func(name, account string) error {
		_, err := ti.service.UpdateGcpIamPlatformCredential(withAdmin(t, ctx, ti), &adminecgen.UpdateGcpIamPlatformCredentialPayload{
			ID: cred.ID, SessionToken: nil, Name: name, ImpersonateServiceAccount: conv.PtrEmpty(account), WifPoolID: nil, WifProviderID: nil, WifProjectNumber: nil,
		})
		return err //nolint:wrapcheck // Test helper.
	}
	remove := func() error {
		return ti.service.DeleteGcpIamPlatformCredential(withAdmin(t, ctx, ti), &adminecgen.DeleteGcpIamPlatformCredentialPayload{ID: cred.ID, SessionToken: nil})
	}

	require.NoError(t, update("renamed", signer), "a rename keeps the account")
	require.NoError(t, update("renamed", strings.ToUpper(signer)), "the account is stored lowercased, so a case-only change is no change")
	requireOopsCode(t, update("renamed", "other-"+signer), oops.CodeConflict)
	_, err = ti.service.UpdateGcpIamPlatformCredential(withAdmin(t, ctx, ti), &adminecgen.UpdateGcpIamPlatformCredentialPayload{
		ID: cred.ID, SessionToken: nil, Name: "renamed", ImpersonateServiceAccount: conv.PtrEmpty(signer), WifPoolID: conv.PtrEmpty("pool"), WifProviderID: conv.PtrEmpty("provider"), WifProjectNumber: conv.PtrEmpty("123456"),
	})
	requireOopsCode(t, err, oops.CodeConflict)
	requireOopsCode(t, remove(), oops.CodeConflict)

	_, err = jq.RevokeJsonWebKey(ctx, jwksrepo.RevokeJsonWebKeyParams{ID: key.ID, OrganizationID: ti.orgID})
	require.NoError(t, err)
	// A revoked key still awaiting cleanup keeps its grant.
	requireOopsCode(t, update("renamed", "other-"+signer), oops.CodeConflict)

	require.NoError(t, jq.MarkJsonWebKeyKMSDisabled(ctx, jwksrepo.MarkJsonWebKeyKMSDisabledParams{ID: key.ID, OrganizationID: ti.orgID}))
	require.NoError(t, update("renamed", "other-"+signer))
}

// Only revoked issuer keys reference the credential: deleting it is allowed,
// since their cleanup reads the deleted credential's account.
func TestDeleteGcpIamPlatformCredential_AllowsOnlyRevokedIssuerKeys(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	cred := createPlatformGCPAmbientCredential(t, ctx, ti, "platform-issuer-revoked")
	credID, err := uuid.Parse(cred.ID)
	require.NoError(t, err)
	backing, err := extkeysrepo.New(ti.conn).CreateExternalKey(ctx, extkeysrepo.CreateExternalKeyParams{
		OrganizationID:               conv.ToPGText(ti.orgID),
		ExternalCredentialID:         credID,
		Provider:                     "gcp_kms",
		Algorithm:                    "RS256",
		Name:                         "issuer",
		CustomerGrantReference:       pgtype.Text{String: "", Valid: false},
		IdentityProviderConnectionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	jq := jwksrepo.New(ti.conn)
	set, err := jq.CreateJsonWebKeySet(ctx, jwksrepo.CreateJsonWebKeySetParams{OrganizationID: ti.orgID, ExternalKeyID: backing.ID, Name: "issuer", IdentityProviderConnectionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}})
	require.NoError(t, err)
	_, err = jq.CreateInternalSessionIssuer(ctx, jwksrepo.CreateInternalSessionIssuerParams{OrganizationID: ti.orgID, JsonWebKeySetID: set.ID, Issuer: "https://app.example.com/oauth/idp/org/" + ti.orgID})
	require.NoError(t, err)
	key, err := jq.CreateJsonWebKey(ctx, jwksrepo.CreateJsonWebKeyParams{OrganizationID: ti.orgID, JsonWebKeySetID: set.ID, ExternalKeyID: backing.ID, State: "active", Kid: "kid-1", PublicJwk: []byte(`{"kty":"EC"}`)})
	require.NoError(t, err)
	_, err = jq.RevokeJsonWebKey(ctx, jwksrepo.RevokeJsonWebKeyParams{ID: key.ID, OrganizationID: ti.orgID})
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteGcpIamPlatformCredential(withAdmin(t, ctx, ti), &adminecgen.DeleteGcpIamPlatformCredentialPayload{ID: cred.ID, SessionToken: nil}))
}

// Deleting a missing id is an idempotent no-op.
func TestDeleteGcpIamPlatformCredential_MissingIsNoOp(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	err := ti.service.DeleteGcpIamPlatformCredential(withAdmin(t, ctx, ti), &adminecgen.DeleteGcpIamPlatformCredentialPayload{
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

	err := ti.service.DeleteGcpIamPlatformCredential(withAdmin(t, ctx, ti), &adminecgen.DeleteGcpIamPlatformCredentialPayload{
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
