package oauth_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oauth"
)

// TestValidateAuthorizationRequest_ScopeEncoding guards against a regression
// where a URL-encoded multi-scope value (e.g. "openid%20profile" or
// "openid+profile") arriving at the OAuth proxy callback would fail scope
// validation because strings.Fields wouldn't split on the encoded separator.
func TestValidateAuthorizationRequest_ScopeEncoding(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newTokenTestEnv(t)

	client, err := env.clientReg.RegisterClient(ctx, &oauth.ClientInfo{
		ClientName:   "test-client",
		RedirectURIs: []string{testRedirectURI},
		GrantTypes:   []string{"authorization_code", "refresh_token"},
		Scope:        "openid profile",
	}, testMCPURL)
	require.NoError(t, err)

	tests := []struct {
		name  string
		scope string
	}{
		{"url-encoded space (%20)", "openid%20profile"},
		{"url-encoded space (+)", "openid+profile"},
		{"plain space-separated", "openid profile"},
		{"single scope", "openid"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			grant, err := env.grantMgr.CreateAuthorizationGrant(
				ctx,
				&oauth.AuthorizationRequest{
					ResponseType: "code",
					ClientID:     client.ClientID,
					RedirectURI:  testRedirectURI,
					Scope:        tc.scope,
				},
				testMCPURL,
				uuid.New(),
				"upstream-access-token",
				"",
				nil,
				nil,
			)
			require.NoError(t, err)
			require.NotNil(t, grant)
			require.NotEmpty(t, grant.Code)
		})
	}
}
