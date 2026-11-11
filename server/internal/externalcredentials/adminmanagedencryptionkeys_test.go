package externalcredentials_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	adminecgen "github.com/speakeasy-api/gram/server/gen/admin_external_credentials"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	oerepo "github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
)

// Speakeasy-managed organization encryption keys pin the platform credential's
// id and impersonated account: its identity cannot change and it cannot be
// deleted while one still reaches KMS through it. Renames stay allowed, and a
// key the cleanup sweep has disabled no longer counts.
func TestGcpIamPlatformCredential_GuardsManagedEncryptionKeys(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	account := gramProjectServiceAccount("org-encryption")
	cred, err := ti.service.CreateGcpIamPlatformCredential(withAdmin(t, ctx, ti), &adminecgen.CreateGcpIamPlatformCredentialPayload{
		SessionToken: nil, Name: "platform-encryption", ImpersonateServiceAccount: conv.PtrEmpty(account),
		WifPoolID: nil, WifProviderID: nil, WifProjectNumber: nil,
	})
	require.NoError(t, err)
	credID, err := uuid.Parse(cred.ID)
	require.NoError(t, err)

	q := oerepo.New(ti.conn)
	key, err := q.CreateEncryptionExternalKey(ctx, oerepo.CreateEncryptionExternalKeyParams{
		OrganizationID:       conv.ToPGText(ti.orgID),
		ExternalCredentialID: credID,
		Algorithm:            "GOOGLE_SYMMETRIC_ENCRYPTION",
		Name:                 "managed-kek",
	})
	require.NoError(t, err)
	require.NoError(t, q.CreateManagedEncryptionKey(ctx, oerepo.CreateManagedEncryptionKeyParams{ExternalKeyID: key.ID, OrganizationID: ti.orgID}))

	update := func(name, target string) error {
		_, err := ti.service.UpdateGcpIamPlatformCredential(withAdmin(t, ctx, ti), &adminecgen.UpdateGcpIamPlatformCredentialPayload{
			ID: cred.ID, SessionToken: nil, Name: name, ImpersonateServiceAccount: conv.PtrEmpty(target), WifPoolID: nil, WifProviderID: nil, WifProjectNumber: nil,
		})
		return err //nolint:wrapcheck // Test helper.
	}
	remove := func() error {
		return ti.service.DeleteGcpIamPlatformCredential(withAdmin(t, ctx, ti), &adminecgen.DeleteGcpIamPlatformCredentialPayload{ID: cred.ID, SessionToken: nil})
	}

	require.NoError(t, update("renamed", account), "a rename keeps the identity")
	requireOopsCode(t, update("renamed", gramProjectServiceAccount("other")), oops.CodeConflict)
	requireOopsCode(t, remove(), oops.CodeConflict)

	require.NoError(t, q.MarkManagedEncryptionKeyDisabled(ctx, oerepo.MarkManagedEncryptionKeyDisabledParams{ExternalKeyID: key.ID, OrganizationID: ti.orgID}))
	require.NoError(t, update("renamed", gramProjectServiceAccount("other")))
	require.NoError(t, remove(), "a disabled key no longer blocks the delete")
}
