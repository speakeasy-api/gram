package internalsessionissuers_test

import (
	"net/url"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/internalsessionissuers"
	"github.com/stretchr/testify/require"
)

func TestDeploymentRequiresExactIssuerAndKeyRing(t *testing.T) {
	t.Parallel()
	origin, err := url.Parse("https://preview.example.com")
	require.NoError(t, err)
	deployment := internalsessionissuers.Deployment{ServerURL: origin, KeyRing: "projects/preview/locations/global/keyRings/issuer"}
	require.NoError(t, deployment.CheckIssuer("org_test", "https://preview.example.com/oauth/idp/org/org_test"))
	require.NoError(t, deployment.CheckKey("projects/preview/locations/global/keyRings/issuer/cryptoKeys/emaissuer-key/cryptoKeyVersions/1"))
	for _, issuer := range []string{"https://source.example.com/oauth/idp/org/org_test", "https://preview.example.com/oauth/idp/org/org_other", "https://preview.example.com/oauth/idp/org/org_test/"} {
		require.ErrorIs(t, deployment.CheckIssuer("org_test", issuer), internalsessionissuers.ErrMisconfigured)
	}
	for _, key := range []string{"projects/source/locations/global/keyRings/issuer/cryptoKeys/emaissuer-key/cryptoKeyVersions/1", "projects/preview/locations/global/keyRings/issuer-other/cryptoKeys/emaissuer-key/cryptoKeyVersions/1", "projects/preview/locations/global/keyRings/issuer/cryptoKeys/key"} {
		require.ErrorIs(t, deployment.CheckKey(key), internalsessionissuers.ErrMisconfigured)
	}
	empty := internalsessionissuers.Deployment{}
	require.ErrorIs(t, empty.CheckIssuer("org_test", "https://preview.example.com/oauth/idp/org/org_test"), internalsessionissuers.ErrMisconfigured)
	require.ErrorIs(t, empty.CheckKey("projects/preview/locations/global/keyRings/issuer/cryptoKeys/emaissuer-key/cryptoKeyVersions/1"), internalsessionissuers.ErrMisconfigured)
}
