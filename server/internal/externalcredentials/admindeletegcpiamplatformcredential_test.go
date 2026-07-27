package externalcredentials_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	adminecgen "github.com/speakeasy-api/gram/server/gen/admin_external_credentials"
	gen "github.com/speakeasy-api/gram/server/gen/external_credentials"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestDeleteGcpIamPlatformCredential_SoftDeletes(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	cred := createPlatformGCPAmbientCredential(t, ctx, ti, "platform-delete")

	err := ti.service.DeleteGcpIamPlatformCredential(withAdmin(t, ctx), &adminecgen.DeleteGcpIamPlatformCredentialPayload{
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

// Deleting a missing id is an idempotent no-op.
func TestDeleteGcpIamPlatformCredential_MissingIsNoOp(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	err := ti.service.DeleteGcpIamPlatformCredential(withAdmin(t, ctx), &adminecgen.DeleteGcpIamPlatformCredentialPayload{
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

	err := ti.service.DeleteGcpIamPlatformCredential(withAdmin(t, ctx), &adminecgen.DeleteGcpIamPlatformCredentialPayload{
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
