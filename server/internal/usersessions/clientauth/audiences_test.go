package clientauth_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/usersessions/clientauth"
)

// Audiences.Match reports the issuer label when a value is both the issuer
// identifier and an endpoint URL, so the canonical name wins.
func TestAudiences_MatchPrefersIssuerLabel(t *testing.T) {
	t.Parallel()

	audiences := clientauth.Audiences{Issuer: testIssuer, Endpoint: testIssuer}
	kind, ok := audiences.Match([]string{testIssuer})
	require.True(t, ok)
	require.Equal(t, clientauth.AudienceKindIssuer, kind)
}
