package externalcredentials_test

import (
	"testing"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/external_credentials"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestExternalCredentials_ListForbiddenWithoutGrants(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.ListExternalCredentials(authztest.WithExactGrants(t, ctx), &gen.ListExternalCredentialsPayload{
		Provider:     nil,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestExternalCredentials_GetForbiddenWithoutGrants(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.GetAwsIamCredential(authztest.WithExactGrants(t, ctx), &gen.GetAwsIamCredentialPayload{
		ID:           uuid.NewString(),
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestExternalCredentials_CreateForbiddenWithoutGrants(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.CreateGcpIamCredential(authztest.WithExactGrants(t, ctx), &gen.CreateGcpIamCredentialPayload{
		SessionToken:              nil,
		Name:                      "denied",
		ImpersonateServiceAccount: nil,
		WifPoolID:                 nil,
		WifProviderID:             nil,
		WifProjectNumber:          nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestExternalCredentials_DeleteForbiddenWithoutGrants(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	err := ti.service.DeleteAwsIamCredential(authztest.WithExactGrants(t, ctx), &gen.DeleteAwsIamCredentialPayload{
		ID:           uuid.NewString(),
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
