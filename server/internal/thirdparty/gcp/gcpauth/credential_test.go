package gcpauth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Credential.mode is the pure decision the resolver makes before any GCP call,
// so it carries the mode-selection coverage the network paths cannot get in CI.
func TestCredentialMode_Ambient(t *testing.T) {
	t.Parallel()

	require.Equal(t, modeAmbient, Credential{}.mode())
}

func TestCredentialMode_Impersonation(t *testing.T) {
	t.Parallel()

	require.Equal(t, modeImpersonation, Credential{
		ImpersonateServiceAccount: "gram@customer.iam.gserviceaccount.com",
	}.mode())
}

func TestCredentialMode_WIF(t *testing.T) {
	t.Parallel()

	require.Equal(t, modeWIF, Credential{WifPoolID: "pool"}.mode())
	require.Equal(t, modeWIF, Credential{WifProviderID: "provider"}.mode())
	require.Equal(t, modeWIF, Credential{WifProjectNumber: "123456789"}.mode())
}

// A WIF credential may also carry an impersonation target as the federation hop;
// WIF still wins so the resolver reports it as unsupported rather than silently
// impersonating.
func TestCredentialMode_WIFWinsOverImpersonationHop(t *testing.T) {
	t.Parallel()

	require.Equal(t, modeWIF, Credential{
		ImpersonateServiceAccount: "gram@customer.iam.gserviceaccount.com",
		WifPoolID:                 "pool",
		WifProviderID:             "provider",
		WifProjectNumber:          "123456789",
	}.mode())
}
