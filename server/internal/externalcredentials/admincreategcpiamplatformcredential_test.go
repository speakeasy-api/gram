package externalcredentials_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	adminecgen "github.com/speakeasy-api/gram/server/gen/admin_external_credentials"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestCreateGcpIamPlatformCredential_Ambient(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	cred, err := ti.service.CreateGcpIamPlatformCredential(withAdmin(t, ctx), &adminecgen.CreateGcpIamPlatformCredentialPayload{
		SessionToken:              nil,
		Name:                      "platform-ambient",
		ImpersonateServiceAccount: nil,
		WifPoolID:                 nil,
		WifProviderID:             nil,
		WifProjectNumber:          nil,
	})
	require.NoError(t, err)
	require.NotNil(t, cred)

	require.Equal(t, "gcp_iam", cred.Provider)
	require.Equal(t, "platform-ambient", cred.Name)
	require.Empty(t, cred.OrganizationID, "a platform credential must not be scoped to an organization")
	require.Nil(t, cred.ImpersonateServiceAccount, "ambient credential has no impersonation target")
	require.Nil(t, cred.WifPoolID)
	_, parseErr := uuid.Parse(cred.ID)
	require.NoError(t, parseErr)
}

func TestCreateGcpIamPlatformCredential_NameRequired(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.CreateGcpIamPlatformCredential(withAdmin(t, ctx), &adminecgen.CreateGcpIamPlatformCredentialPayload{
		SessionToken:              nil,
		Name:                      "   ",
		ImpersonateServiceAccount: nil,
		WifPoolID:                 nil,
		WifProviderID:             nil,
		WifProjectNumber:          nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateGcpIamPlatformCredential_WifTripleMustBeComplete(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.CreateGcpIamPlatformCredential(withAdmin(t, ctx), &adminecgen.CreateGcpIamPlatformCredentialPayload{
		SessionToken:              nil,
		Name:                      "partial-wif",
		ImpersonateServiceAccount: nil,
		WifPoolID:                 new("pool"),
		WifProviderID:             nil,
		WifProjectNumber:          nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}
