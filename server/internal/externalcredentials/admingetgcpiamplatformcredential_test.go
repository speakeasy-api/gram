package externalcredentials_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	adminecgen "github.com/speakeasy-api/gram/server/gen/admin_external_credentials"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestGetGcpIamPlatformCredential_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createPlatformGCPAmbientCredential(t, ctx, ti, "platform-get")

	got, err := ti.service.GetGcpIamPlatformCredential(withAdmin(t, ctx), &adminecgen.GetGcpIamPlatformCredentialPayload{
		ID:           created.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, "platform-get", got.Name)
	require.Empty(t, got.OrganizationID)
}

func TestGetGcpIamPlatformCredential_NotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.GetGcpIamPlatformCredential(withAdmin(t, ctx), &adminecgen.GetGcpIamPlatformCredentialPayload{
		ID:           uuid.NewString(),
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetGcpIamPlatformCredential_InvalidID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.GetGcpIamPlatformCredential(withAdmin(t, ctx), &adminecgen.GetGcpIamPlatformCredentialPayload{
		ID:           "not-a-uuid",
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// An organization-scoped credential is not reachable through the platform get:
// the platform query pins organization_id IS NULL AND project_id IS NULL.
func TestGetGcpIamPlatformCredential_ExcludesOrgCredential(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	orgCred := createGCPImpersonationCredential(t, ctx, ti, "org-scoped")

	_, err := ti.service.GetGcpIamPlatformCredential(withAdmin(t, ctx), &adminecgen.GetGcpIamPlatformCredentialPayload{
		ID:           orgCred.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}
