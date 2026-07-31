package gcpkms

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// testResourceName is the well-formed key version every test in this package
// addresses. It lives here because this file owns resource-name validation.
const testResourceName = "projects/gram-test/locations/us-central1/keyRings/signing/cryptoKeys/jwks/cryptoKeyVersions/1"

func TestValidateKeyVersionName_AcceptsKeyVersion(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateKeyVersionName(testResourceName))
}

// An asymmetric-sign key has no primary version, so a key-level path cannot be
// resolved to something signable and must be rejected up front.
func TestValidateKeyVersionName_RejectsVersionlessKeyPath(t *testing.T) {
	t.Parallel()

	err := ValidateKeyVersionName("projects/p/locations/l/keyRings/r/cryptoKeys/k")
	require.ErrorIs(t, err, ErrInvalidResourceName)
}

func TestValidateKeyVersionName_RejectsMalformed(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"",
		"not-a-resource-name",
		"projects//locations/l/keyRings/r/cryptoKeys/k/cryptoKeyVersions/1",
		"projects/p/locations/l/keyRings/r/cryptoKeys/k/cryptoKeyVersions/",
		"projects/p/locations/l/keyRings/r/cryptoKeys/k/cryptoKeyVersions/1/extra",
		"/projects/p/locations/l/keyRings/r/cryptoKeys/k/cryptoKeyVersions/1",
		"projects/p/locations/l/keyRings/r/cryptoKeys/k/cryptoKeyVersions/1 ",
	} {
		require.ErrorIs(t, ValidateKeyVersionName(name), ErrInvalidResourceName, "should reject %q", name)
	}
}
