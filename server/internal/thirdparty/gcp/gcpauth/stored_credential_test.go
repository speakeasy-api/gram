package gcpauth_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
)

// gramProjectTarget is a user-managed service account inside the same project as
// StubResolverPrincipal, so the screening places it in Gram's own project.
const gramProjectTarget = "internal@gram-stub.iam.gserviceaccount.com"

func TestScreenStoredCredential_AcceptsCustomerTarget(t *testing.T) {
	t.Parallel()

	identity := gcpauth.NewIdentity(gcpauth.NewStubResolver())

	credential, problem, detail, err := identity.ScreenStoredCredential(t.Context(), testenv.NewLogger(t), gcpauth.StoredCredential{
		Present:                   true,
		ImpersonateServiceAccount: "signer@customer-project.iam.gserviceaccount.com",
		HasWifConfig:              false,
		SkipProjectVerification:   false,
	})
	require.NoError(t, err)
	require.Empty(t, problem)
	require.Empty(t, detail)
	require.Equal(t, "signer@customer-project.iam.gserviceaccount.com", credential.ImpersonateServiceAccount)
}

// The outbound path has no request actor, so the exemption a platform
// administrator granted has to be readable from the row. Without this the
// credential would save and then be refused on every mint and every verify.
func TestScreenStoredCredential_ExemptionAllowsGramProjectTarget(t *testing.T) {
	t.Parallel()

	identity := gcpauth.NewIdentity(gcpauth.NewStubResolver())

	credential, problem, detail, err := identity.ScreenStoredCredential(t.Context(), testenv.NewLogger(t), gcpauth.StoredCredential{
		Present:                   true,
		ImpersonateServiceAccount: gramProjectTarget,
		HasWifConfig:              false,
		SkipProjectVerification:   true,
	})
	require.NoError(t, err)
	require.Empty(t, problem)
	require.Empty(t, detail)
	require.Equal(t, gramProjectTarget, credential.ImpersonateServiceAccount)
}

func TestScreenStoredCredential_RefusesGramProjectTargetWithoutExemption(t *testing.T) {
	t.Parallel()

	identity := gcpauth.NewIdentity(gcpauth.NewStubResolver())

	_, problem, detail, err := identity.ScreenStoredCredential(t.Context(), testenv.NewLogger(t), gcpauth.StoredCredential{
		Present:                   true,
		ImpersonateServiceAccount: gramProjectTarget,
		HasWifConfig:              false,
		SkipProjectVerification:   false,
	})
	require.NoError(t, err)
	require.Equal(t, gcpauth.StoredCredentialUnusable, problem)
	require.Contains(t, detail, "your own GCP project")
}

// The exemption covers the own-project refusal and nothing else. A target that
// cannot be placed in a project at all was never compared against Gram's, so
// forgiving it would widen the grant to addresses no administrator approved.
func TestScreenStoredCredential_ExemptionDoesNotForgiveMalformedTarget(t *testing.T) {
	t.Parallel()

	identity := gcpauth.NewIdentity(gcpauth.NewStubResolver())

	for _, target := range []string{
		"123456789012-compute@developer.gserviceaccount.com",
		"person@example.com",
		"service-123456789012@gcp-sa-cloudkms.iam.gserviceaccount.com",
	} {
		_, problem, detail, err := identity.ScreenStoredCredential(t.Context(), testenv.NewLogger(t), gcpauth.StoredCredential{
			Present:                   true,
			ImpersonateServiceAccount: target,
			HasWifConfig:              false,
			SkipProjectVerification:   true,
		})
		require.NoError(t, err, "%q", target)
		require.Equal(t, gcpauth.StoredCredentialUnusable, problem, "%q must stay refused", target)
		require.Contains(t, detail, "user-managed service account", "%q", target)
	}
}

// The steps above the screening are more fundamental than it, so the exemption
// must not carry a row past them.
func TestScreenStoredCredential_ExemptionDoesNotForgiveUnusableRows(t *testing.T) {
	t.Parallel()

	identity := gcpauth.NewIdentity(gcpauth.NewStubResolver())
	logger := testenv.NewLogger(t)

	_, problem, detail, err := identity.ScreenStoredCredential(t.Context(), logger, gcpauth.StoredCredential{
		Present:                   false,
		ImpersonateServiceAccount: gramProjectTarget,
		HasWifConfig:              false,
		SkipProjectVerification:   true,
	})
	require.NoError(t, err)
	require.Equal(t, gcpauth.StoredCredentialDeleted, problem)
	require.Contains(t, detail, "deleted")

	_, problem, detail, err = identity.ScreenStoredCredential(t.Context(), logger, gcpauth.StoredCredential{
		Present:                   true,
		ImpersonateServiceAccount: "",
		HasWifConfig:              false,
		SkipProjectVerification:   true,
	})
	require.NoError(t, err)
	require.Equal(t, gcpauth.StoredCredentialUnusable, problem)
	require.Contains(t, detail, "names no service account")

	_, problem, detail, err = identity.ScreenStoredCredential(t.Context(), logger, gcpauth.StoredCredential{
		Present:                   true,
		ImpersonateServiceAccount: gramProjectTarget,
		HasWifConfig:              true,
		SkipProjectVerification:   true,
	})
	require.NoError(t, err)
	require.Equal(t, gcpauth.StoredCredentialUnusable, problem)
	require.Contains(t, detail, "Workload Identity Federation")
}
