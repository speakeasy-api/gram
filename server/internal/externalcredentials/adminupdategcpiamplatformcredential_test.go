package externalcredentials_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	adminecgen "github.com/speakeasy-api/gram/server/gen/admin_external_credentials"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestUpdateGcpIamPlatformCredential_ReplacesConfig(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createPlatformGCPAmbientCredential(t, ctx, ti, "platform-update")

	updated, err := ti.service.UpdateGcpIamPlatformCredential(withAdmin(t, ctx), &adminecgen.UpdateGcpIamPlatformCredentialPayload{
		ID:                        created.ID,
		SessionToken:              nil,
		Name:                      "platform-update-renamed",
		ImpersonateServiceAccount: new("gram@gram-platform.iam.gserviceaccount.com"),
		WifPoolID:                 nil,
		WifProviderID:             nil,
		WifProjectNumber:          nil,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, created.ID, updated.ID)
	require.Equal(t, "platform-update-renamed", updated.Name)
	require.NotNil(t, updated.ImpersonateServiceAccount)
	require.Equal(t, "gram@gram-platform.iam.gserviceaccount.com", *updated.ImpersonateServiceAccount)

	// The change is persisted (full replace).
	got, err := ti.service.GetGcpIamPlatformCredential(withAdmin(t, ctx), &adminecgen.GetGcpIamPlatformCredentialPayload{
		ID:           created.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "platform-update-renamed", got.Name)
	require.NotNil(t, got.ImpersonateServiceAccount)
}

func TestUpdateGcpIamPlatformCredential_NameRequired(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createPlatformGCPAmbientCredential(t, ctx, ti, "platform-update-noname")

	_, err := ti.service.UpdateGcpIamPlatformCredential(withAdmin(t, ctx), &adminecgen.UpdateGcpIamPlatformCredentialPayload{
		ID:                        created.ID,
		SessionToken:              nil,
		Name:                      "   ",
		ImpersonateServiceAccount: nil,
		WifPoolID:                 nil,
		WifProviderID:             nil,
		WifProjectNumber:          nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestUpdateGcpIamPlatformCredential_NotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.UpdateGcpIamPlatformCredential(withAdmin(t, ctx), &adminecgen.UpdateGcpIamPlatformCredentialPayload{
		ID:                        uuid.NewString(),
		SessionToken:              nil,
		Name:                      "nope",
		ImpersonateServiceAccount: nil,
		WifPoolID:                 nil,
		WifProviderID:             nil,
		WifProjectNumber:          nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

// An organization-scoped credential is not reachable through the platform update.
func TestUpdateGcpIamPlatformCredential_ExcludesOrgCredential(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	orgCred := createGCPImpersonationCredential(t, ctx, ti, "org-scoped")

	_, err := ti.service.UpdateGcpIamPlatformCredential(withAdmin(t, ctx), &adminecgen.UpdateGcpIamPlatformCredentialPayload{
		ID:                        orgCred.ID,
		SessionToken:              nil,
		Name:                      "hijack",
		ImpersonateServiceAccount: nil,
		WifPoolID:                 nil,
		WifProviderID:             nil,
		WifProjectNumber:          nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}
