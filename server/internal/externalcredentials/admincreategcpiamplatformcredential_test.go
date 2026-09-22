package externalcredentials_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	adminecgen "github.com/speakeasy-api/gram/server/gen/admin_external_credentials"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/externalcredentials/repo"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/provisiontest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestCreateGcpIamPlatformCredential_Ambient(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	cred, err := ti.service.CreateGcpIamPlatformCredential(withFreshAdmin(t, ctx, ti), &adminecgen.CreateGcpIamPlatformCredentialPayload{
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

	_, err := ti.service.CreateGcpIamPlatformCredential(withFreshAdmin(t, ctx, ti), &adminecgen.CreateGcpIamPlatformCredentialPayload{
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

	_, err := ti.service.CreateGcpIamPlatformCredential(withFreshAdmin(t, ctx, ti), &adminecgen.CreateGcpIamPlatformCredentialPayload{
		SessionToken:              nil,
		Name:                      "partial-wif",
		ImpersonateServiceAccount: nil,
		WifPoolID:                 new("pool"),
		WifProviderID:             nil,
		WifProjectNumber:          nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateGcpIamPlatformCredential_ExemptsOwnProjectSigner(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	cred, err := ti.service.CreateGcpIamPlatformCredential(withFreshAdmin(t, ctx, ti), &adminecgen.CreateGcpIamPlatformCredentialPayload{
		SessionToken:              nil,
		Name:                      "identity-provider-signer",
		ImpersonateServiceAccount: conv.PtrEmpty(provisiontest.SigningServiceAccount()),
		WifPoolID:                 nil,
		WifProviderID:             nil,
		WifProjectNumber:          nil,
	})
	require.NoError(t, err)

	row, err := repo.New(ti.conn).GetGcpIamCredential(ctx, repo.GetGcpIamCredentialParams{ID: uuid.MustParse(cred.ID), OrganizationID: pgtype.Text{}})
	require.NoError(t, err)
	require.True(t, row.GcpIamCredential.SkipProjectVerification, "a signer in Gram's own project must pass the own-project screen")
}

func TestCreateGcpIamPlatformCredential_RejectsStaleAdminFlag(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.CreateGcpIamPlatformCredential(withAdmin(t, ctx), &adminecgen.CreateGcpIamPlatformCredentialPayload{
		SessionToken:              nil,
		Name:                      "platform-ambient",
		ImpersonateServiceAccount: nil,
		WifPoolID:                 nil,
		WifProviderID:             nil,
		WifProjectNumber:          nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
