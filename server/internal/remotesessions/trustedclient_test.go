package remotesessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func trustedIdentityProviderPair() (repo.RemoteSessionClient, repo.RemoteSessionIssuer) {
	var client repo.RemoteSessionClient
	client.ID = uuid.New()
	client.Scope = []string{"openid", "email"}
	client.ClientSecretEncrypted = pgtype.Text{String: "encrypted", Valid: true}
	client.TokenEndpointAuthMethod = pgtype.Text{String: "client_secret_basic", Valid: true}

	var issuer repo.RemoteSessionIssuer
	issuer.ScopesSupported = []string{"openid", "email"}
	issuer.TokenEndpointAuthMethodsSupported = []string{"client_secret_basic"}
	return client, issuer
}

func TestValidateTrustedIdentityProviderClientRejectsExcludedOfflineAccess(t *testing.T) {
	t.Parallel()

	client, issuer := trustedIdentityProviderPair()
	client.Scope = append(client.Scope, "offline_access")

	err := remotesessions.ValidateTrustedIdentityProviderClient(client, issuer)
	require.ErrorContains(t, err, "offline_access")
}

func TestValidateTrustedIdentityProviderClientAllowsOfflineAccessWhenMetadataOmitsScopes(t *testing.T) {
	t.Parallel()

	client, issuer := trustedIdentityProviderPair()
	client.Scope = append(client.Scope, "offline_access")
	issuer.ScopesSupported = nil

	require.NoError(t, remotesessions.ValidateTrustedIdentityProviderClient(client, issuer))
}

func TestValidateTrustedIdentityProviderClientRejectsIncompleteScopeOverride(t *testing.T) {
	t.Parallel()

	client, issuer := trustedIdentityProviderPair()
	issuer.ScopeOverride = []string{"openid"}

	err := remotesessions.ValidateTrustedIdentityProviderClient(client, issuer)
	require.ErrorContains(t, err, "email")
}

func TestValidateTrustedIdentityProviderClientUsesScopeOverrideAsEffectiveScopes(t *testing.T) {
	t.Parallel()

	client, issuer := trustedIdentityProviderPair()
	client.Scope = []string{"offline_access"}
	issuer.ScopeOverride = []string{"openid", "email"}

	require.NoError(t, remotesessions.ValidateTrustedIdentityProviderClient(client, issuer))
}

func TestValidateTrustedIdentityProviderClientRejectsPublicClient(t *testing.T) {
	t.Parallel()

	client, issuer := trustedIdentityProviderPair()
	client.ClientSecretEncrypted = pgtype.Text{}
	client.TokenEndpointAuthMethod = pgtype.Text{String: "none", Valid: true}
	issuer.TokenEndpointAuthMethodsSupported = []string{"none"}

	err := remotesessions.ValidateTrustedIdentityProviderClient(client, issuer)
	require.ErrorContains(t, err, "not eligible")
}

func TestValidateTrustedIdentityProviderClientAllowsPrivateKeyJWT(t *testing.T) {
	t.Parallel()

	client, issuer := trustedIdentityProviderPair()
	client.ClientSecretEncrypted = pgtype.Text{}
	client.TokenEndpointAuthMethod = pgtype.Text{String: "private_key_jwt", Valid: true}
	client.JsonWebKeySetID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
	issuer.TokenEndpointAuthMethodsSupported = []string{"private_key_jwt"}

	require.NoError(t, remotesessions.ValidateTrustedIdentityProviderClient(client, issuer))
}
