package privatekeyjwt_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/privatekeyjwt"
)

// Audiences.Match reports the issuer label when a value is both the issuer
// identifier and an endpoint URL, so the canonical name wins.
func TestAudiences_MatchPrefersIssuerLabel(t *testing.T) {
	t.Parallel()

	audiences := privatekeyjwt.Audiences{Issuer: testIssuer, Endpoint: testIssuer}
	kind, ok := audiences.Match([]string{testIssuer})
	require.True(t, ok)
	require.Equal(t, privatekeyjwt.AudienceKindIssuer, kind)
}

// Audiences.Match accepts the issuer alias under its own label, and an unset
// alias admits nothing.
func TestAudiences_MatchIssuerAlias(t *testing.T) {
	t.Parallel()

	const alias = "https://auth.example.com/mcp/demo"

	audiences := privatekeyjwt.Audiences{Issuer: testIssuer, IssuerAlias: alias, Endpoint: testTokenURL}
	kind, ok := audiences.Match([]string{alias})
	require.True(t, ok)
	require.Equal(t, privatekeyjwt.AudienceKindIssuerAlias, kind)

	withoutAlias := privatekeyjwt.Audiences{Issuer: testIssuer, IssuerAlias: "", Endpoint: testTokenURL}
	_, ok = withoutAlias.Match([]string{alias})
	require.False(t, ok)
	_, ok = withoutAlias.Match([]string{""})
	require.False(t, ok)
}
