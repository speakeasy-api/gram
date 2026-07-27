package externalcredentials_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	adminecgen "github.com/speakeasy-api/gram/server/gen/admin_external_credentials"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// TestAdminExternalCredentials_RejectsNonPlatformAdmin asserts that every
// adminExternalCredentials method fails closed for a caller without the
// platform-admin flag. The default context from newTestService is a non-admin
// org user, so each call must be refused at the requirePlatformAdmin
// choke-point before it touches any data. This guards against a handler being
// added later that forgets the gate.
func TestAdminExternalCredentials_RejectsNonPlatformAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	id := uuid.NewString()

	_, createErr := ti.service.CreateGcpIamPlatformCredential(ctx, &adminecgen.CreateGcpIamPlatformCredentialPayload{
		SessionToken:              nil,
		Name:                      "x",
		ImpersonateServiceAccount: nil,
		WifPoolID:                 nil,
		WifProviderID:             nil,
		WifProjectNumber:          nil,
	})
	_, listErr := ti.service.ListPlatformExternalCredentials(ctx, &adminecgen.ListPlatformExternalCredentialsPayload{
		Provider:     nil,
		SessionToken: nil,
	})
	_, updateErr := ti.service.UpdateGcpIamPlatformCredential(ctx, &adminecgen.UpdateGcpIamPlatformCredentialPayload{
		ID:                        id,
		SessionToken:              nil,
		Name:                      "x",
		ImpersonateServiceAccount: nil,
		WifPoolID:                 nil,
		WifProviderID:             nil,
		WifProjectNumber:          nil,
	})
	_, getErr := ti.service.GetGcpIamPlatformCredential(ctx, &adminecgen.GetGcpIamPlatformCredentialPayload{
		ID:           id,
		SessionToken: nil,
	})
	_, verifyErr := ti.service.VerifyGcpIamPlatformCredential(ctx, &adminecgen.VerifyGcpIamPlatformCredentialPayload{
		ID:           id,
		SessionToken: nil,
	})
	deleteErr := ti.service.DeleteGcpIamPlatformCredential(ctx, &adminecgen.DeleteGcpIamPlatformCredentialPayload{
		ID:           id,
		SessionToken: nil,
	})

	cases := []struct {
		method string
		err    error
	}{
		{"createGcpIam", createErr},
		{"updateGcpIam", updateErr},
		{"list", listErr},
		{"getGcpIam", getErr},
		{"verifyGcpIam", verifyErr},
		{"deleteGcpIam", deleteErr},
	}

	for _, c := range cases {
		require.Error(t, c.err, "method %q must reject a non-platform-admin caller", c.method)
		var oopsErr *oops.ShareableError
		require.ErrorAs(t, c.err, &oopsErr, "method %q", c.method)
		require.Equalf(t, oops.CodeForbidden, oopsErr.Code, "method %q", c.method)
	}
}
