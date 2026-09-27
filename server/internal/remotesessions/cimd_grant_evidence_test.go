package remotesessions

import (
	"encoding/json"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/stretchr/testify/require"
)

func TestClientMetadataDocument_PreservesExplicitGrantEvidence(t *testing.T) {
	t.Parallel()
	const clientID = "https://gram.example/.well-known/oauth-client/client"
	for _, tc := range []struct {
		name                        string
		grants, expected, responses []string
	}{
		{"unknown", nil, []string{}, []string{}},
		{"empty", []string{}, []string{}, []string{}},
		{"jwt", []string{oauthwire.GrantTypeJWTBearer}, []string{oauthwire.GrantTypeJWTBearer}, []string{}},
		{"combined", []string{oauthwire.GrantTypeAuthorizationCode, oauthwire.GrantTypeRefreshToken, oauthwire.GrantTypeJWTBearer}, []string{oauthwire.GrantTypeAuthorizationCode, oauthwire.GrantTypeRefreshToken, oauthwire.GrantTypeJWTBearer}, []string{oauthwire.ResponseTypeCode}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := BuildClientMetadataDocumentWithGrants(clientID, "https://gram.example/callback", TokenEndpointAuthMethodNone, "", nil, tc.grants)
			require.Equal(t, clientID, doc.ClientID)
			require.Equal(t, tc.expected, doc.GrantTypes)
			require.Equal(t, tc.responses, doc.ResponseTypes)
			encoded, err := json.Marshal(doc)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), `"grant_types":null`)
		})
	}
}
