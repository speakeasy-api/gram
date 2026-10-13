package internalsessionissuers

import (
	"testing"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
	"github.com/stretchr/testify/require"
)

func TestSignVerifiesKMSResultAgainstPublishedKey(t *testing.T) {
	t.Parallel()
	published, err := gcpkms.NewLocalSigningClient(jose.RS256)
	require.NoError(t, err)
	other, err := gcpkms.NewLocalSigningClient(jose.RS256)
	require.NoError(t, err)
	resource := "projects/test/locations/global/keyRings/issuer/cryptoKeys/key/cryptoKeyVersions/1"
	public, err := published.GetPublicKey(t.Context(), resource)
	require.NoError(t, err)
	document, err := (&jose.JSONWebKey{Key: public.Key, KeyID: "published", Algorithm: string(public.Algorithm), Use: "sig"}).MarshalJSON()
	require.NoError(t, err)
	assertion, err := sign(t.Context(), other, resource, "published", document, map[string]any{"sub": "test-user"})
	require.ErrorContains(t, err, "verify assertion against published key")
	require.Empty(t, assertion)
	assertion, err = sign(t.Context(), published, resource, "published", document, map[string]any{"sub": "test-user"})
	require.NoError(t, err)
	require.NotEmpty(t, assertion)
}
