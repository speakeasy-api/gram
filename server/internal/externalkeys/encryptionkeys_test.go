package externalkeys_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/external_keys"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	orgencryptionrepo "github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
)

// Encryption keys are external_keys rows too, but they protect organization
// data and are managed through organization encryption. This API must neither
// list them nor let them be read, changed or deleted.
func TestExternalKeys_HideEncryptionKeys(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	encryptionKey := orgencryptiontest.CreateCustomerKey(t, ti.conn, ti.orgID, "customer-kek")

	reader := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource))
	admin := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource))

	result, err := ti.service.ListExternalKeys(reader, &gen.ListExternalKeysPayload{Provider: nil, SessionToken: nil})
	require.NoError(t, err)
	require.NotContains(t, keyIDs(result), encryptionKey.String())

	_, err = ti.service.GetGcpKmsKey(reader, &gen.GetGcpKmsKeyPayload{ID: encryptionKey.String(), SessionToken: nil})
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = ti.service.UpdateGcpKmsKey(admin, &gen.UpdateGcpKmsKeyPayload{
		ID:                     encryptionKey.String(),
		ExternalCredentialID:   createGcpIamCredential(t, ctx, ti, "gcp-cred"),
		Name:                   "renamed",
		CustomerGrantReference: nil,
		SessionToken:           nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	require.NoError(t, ti.service.DeleteGcpKmsKey(admin, &gen.DeleteGcpKmsKeyPayload{ID: encryptionKey.String(), SessionToken: nil}))
	_, err = orgencryptionrepo.New(ti.conn).GetEncryptionExternalKey(t.Context(), orgencryptionrepo.GetEncryptionExternalKeyParams{ID: encryptionKey, OrganizationID: ti.orgID})
	require.NoError(t, err, "deleting through this API leaves the encryption key alone")
}
