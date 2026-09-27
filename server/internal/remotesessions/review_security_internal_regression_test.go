package remotesessions

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/stretchr/testify/require"
)

func TestReviewSecurityRegistrationRejectsSingleConnection(t *testing.T) {
	t.Parallel()
	config, err := pgxpool.ParseConfig("postgres://localhost/postgres?sslmode=disable")
	require.NoError(t, err)
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	require.NoError(t, err)
	defer pool.Close()
	release, err := admitRegistration(context.Background(), pool)
	require.ErrorContains(t, err, "at least two connections")
	require.Nil(t, release)
	require.Zero(t, pool.Stat().AcquiredConns())
	registrationAdmissions.Lock()
	_, registered := registrationAdmissions.pools[pool]
	registrationAdmissions.Unlock()
	require.False(t, registered)
}

func TestReviewSecurityCIMDUnknownGrantsRemainUnknown(t *testing.T) {
	t.Parallel()
	for _, grants := range [][]string{nil, {}} {
		doc := BuildClientMetadataDocumentWithGrants("https://example.com/client", "https://example.com/callback", TokenEndpointAuthMethodNone, "", nil, grants)
		require.Empty(t, doc.GrantTypes)
		require.NotNil(t, doc.GrantTypes)
		require.Empty(t, doc.ResponseTypes)
	}
	legacy := BuildClientMetadataDocument("https://example.com/client", "https://example.com/callback", TokenEndpointAuthMethodNone, "", nil)
	require.Equal(t, []string{oauthwire.GrantTypeAuthorizationCode, oauthwire.GrantTypeRefreshToken}, legacy.GrantTypes)
	grants := []string{oauthwire.GrantTypeJWTBearer}
	doc := BuildClientMetadataDocumentWithGrants("https://example.com/client", "https://example.com/callback", TokenEndpointAuthMethodNone, "", nil, grants)
	require.Equal(t, grants, doc.GrantTypes)
	grants[0] = oauthwire.GrantTypeAuthorizationCode
	require.Equal(t, []string{oauthwire.GrantTypeJWTBearer}, doc.GrantTypes)
}
