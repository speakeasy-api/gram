package externalkeys_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/external_keys"
	"github.com/speakeasy-api/gram/server/internal/conv"
	extcredrepo "github.com/speakeasy-api/gram/server/internal/externalcredentials/repo"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/provisiontest"
	jwksrepo "github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// provisionManagedKey runs the real identity provider connection provisioner
// against this package's test organization, yielding an external key that
// carries the managed-by marker and is backed by the platform credential.
func provisionManagedKey(t *testing.T, ctx context.Context, ti *testInstance) *provisiontest.Fixture {
	t.Helper()

	issuerID := provisiontest.CreateIssuer(t, ctx, ti.conn, ti.orgID, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, provisiontest.TokenEndpoint)

	return provisiontest.Provision(t, ctx, ti.conn, ti.orgID, issuerID, "https://app.getgram.ai")
}

// A managed key lists and reads through the organization surface, but cannot
// be re-pointed, deleted, or probed: probing would sign as Speakeasy's
// identity on the organization's request.
func TestManagedKey_RefusesOrganizationTierMutations(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	fx := provisionManagedKey(t, ctx, ti)
	keyID := fx.Client.ExternalKeyID.String()

	got, err := ti.service.GetGcpKmsKey(adminCtx(t, ctx), &gen.GetGcpKmsKeyPayload{ID: keyID, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, keyID, got.ID)

	listed, err := ti.service.ListGcpKmsKeys(adminCtx(t, ctx), &gen.ListGcpKmsKeysPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Contains(t, keyIDs(listed), keyID)

	ownCredential := createGcpIamCredential(t, ctx, ti, "managed-guard-cred")
	_, err = ti.service.UpdateGcpKmsKey(adminCtx(t, ctx), &gen.UpdateGcpKmsKeyPayload{
		ID:                     keyID,
		SessionToken:           nil,
		ExternalCredentialID:   ownCredential,
		Name:                   "hijacked",
		CustomerGrantReference: nil,
	})
	requireOopsCode(t, err, oops.CodeConflict)

	_, err = ti.service.VerifyGcpKmsKey(adminCtx(t, ctx), &gen.VerifyGcpKmsKeyPayload{ID: keyID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeConflict)

	err = ti.service.DeleteGcpKmsKey(adminCtx(t, ctx), &gen.DeleteGcpKmsKeyPayload{ID: keyID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeConflict)

	still, err := ti.service.GetGcpKmsKey(adminCtx(t, ctx), &gen.GetGcpKmsKeyPayload{ID: keyID, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, got.ExternalCredentialID, still.ExternalCredentialID, "the backing credential must not have moved")
}

// Defense in depth for the credential tier decision: an organization key can
// never sit behind a credential exempted from the own-project screening, on
// create or on update. Speakeasy's own keys go through the identity provider
// connection path against a platform-tier credential instead.
func TestCreateGcpKmsKey_RefusesExemptedCredential(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	exempted := createGcpIamCredentialDirect(t, ctx, ti, "staff-exempted", extcredrepo.CreateGcpIamCredentialParams{
		ExternalCredentialID:      uuid.Nil,
		ImpersonateServiceAccount: conv.ToPGText(gramProjectServiceAccount("internal")),
		WifPoolID:                 pgtype.Text{String: "", Valid: false},
		WifProviderID:             pgtype.Text{String: "", Valid: false},
		WifProjectNumber:          pgtype.Text{String: "", Valid: false},
		SkipProjectVerification:   true,
	})

	_, err := ti.service.CreateGcpKmsKey(adminCtx(t, ctx), &gen.CreateGcpKmsKeyPayload{
		SessionToken:           nil,
		ResourceName:           "projects/gram/locations/global/keyRings/signing/cryptoKeys/" + uuid.NewString() + "/cryptoKeyVersions/1",
		ExternalCredentialID:   exempted,
		Algorithm:              "ES256",
		Name:                   "behind-exempted",
		CustomerGrantReference: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.ErrorContains(t, err, "exempted from project verification")

	ordinary := createGcpIamCredential(t, ctx, ti, "ordinary")
	key := createGcpKmsKey(t, ctx, ti, "behind-ordinary", ordinary)

	_, err = ti.service.UpdateGcpKmsKey(adminCtx(t, ctx), &gen.UpdateGcpKmsKeyPayload{
		ID:                     key.ID,
		SessionToken:           nil,
		ExternalCredentialID:   exempted,
		Name:                   key.Name,
		CustomerGrantReference: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// Once the connection is tombstoned its key drops out of the list and becomes
// deletable after its set is gone, while re-pointing and probing stay refused.
func TestManagedKey_DeletableOnceConnectionTombstoned(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	fx := provisionManagedKey(t, ctx, ti)
	keyID := fx.Client.ExternalKeyID.String()

	err := ti.service.DeleteGcpKmsKey(adminCtx(t, ctx), &gen.DeleteGcpKmsKeyPayload{ID: keyID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeConflict)

	provisiontest.SoftDeleteConnection(t, ctx, ti.conn, ti.orgID, fx.ConnectionID)

	listed, err := ti.service.ListGcpKmsKeys(adminCtx(t, ctx), &gen.ListGcpKmsKeysPayload{SessionToken: nil})
	require.NoError(t, err)
	require.NotContains(t, keyIDs(listed), keyID, "a leftover key is hidden from the list")
	got, err := ti.service.GetGcpKmsKey(adminCtx(t, ctx), &gen.GetGcpKmsKeyPayload{ID: keyID, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, keyID, got.ID, "the leftover stays reachable by id")

	ownCredential := createGcpIamCredential(t, ctx, ti, "managed-tombstone-cred")
	_, err = ti.service.UpdateGcpKmsKey(adminCtx(t, ctx), &gen.UpdateGcpKmsKeyPayload{
		ID:                     keyID,
		SessionToken:           nil,
		ExternalCredentialID:   ownCredential,
		Name:                   "hijacked",
		CustomerGrantReference: nil,
	})
	requireOopsCode(t, err, oops.CodeConflict)
	_, err = ti.service.VerifyGcpKmsKey(adminCtx(t, ctx), &gen.VerifyGcpKmsKeyPayload{ID: keyID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeConflict)

	// The leftover set and its key still reference the external key; they go first.
	err = ti.service.DeleteGcpKmsKey(adminCtx(t, ctx), &gen.DeleteGcpKmsKeyPayload{ID: keyID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeConflict)
	require.ErrorContains(t, err, "still in use")
	_, err = fx.Provisioner.RevokeClient(ctx, ti.orgID, fx.ConnectionID)
	require.NoError(t, err)
	_, err = jwksrepo.New(ti.conn).SoftDeleteJsonWebKeySet(ctx, jwksrepo.SoftDeleteJsonWebKeySetParams{ID: fx.Client.JSONWebKeySetID, OrganizationID: ti.orgID})
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteGcpKmsKey(adminCtx(t, ctx), &gen.DeleteGcpKmsKeyPayload{ID: keyID, SessionToken: nil}))
	_, err = ti.service.GetGcpKmsKey(adminCtx(t, ctx), &gen.GetGcpKmsKeyPayload{ID: keyID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
}
