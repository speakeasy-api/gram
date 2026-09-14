package urn_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestNewSystemPrincipal(t *testing.T) {
	t.Parallel()

	p := urn.NewSystemPrincipal("issuer-metadata-refresh")
	require.Equal(t, urn.PrincipalTypeSystem, p.Type)
	require.Equal(t, "issuer-metadata-refresh", p.ID)
	require.Equal(t, "system:issuer-metadata-refresh", p.String())
	require.Equal(t, `system "issuer-metadata-refresh"`, p.Label())

	value, err := p.Value()
	require.NoError(t, err)
	require.Equal(t, "system:issuer-metadata-refresh", value)

	text, err := p.MarshalText()
	require.NoError(t, err)
	require.Equal(t, "system:issuer-metadata-refresh", string(text))
}

func TestNewSystemPrincipal_RejectsNonComponentIDs(t *testing.T) {
	t.Parallel()

	for _, id := range []string{"", "Issuer-Refresh", "issuer refresh", "-leading", "trailing-", "a--b", "user_01abc", "dev@example.com"} {
		_, err := urn.NewSystemPrincipal(id).MarshalJSON()
		require.ErrorIs(t, err, urn.ErrInvalid, "id %q", id)
	}
}

func TestParsePrincipal_SystemRoundTrip(t *testing.T) {
	t.Parallel()

	parsed, err := urn.ParsePrincipal("system:issuer-metadata-refresh")
	require.NoError(t, err)
	require.Equal(t, urn.NewSystemPrincipal("issuer-metadata-refresh"), parsed)

	_, err = urn.ParsePrincipal("system:Not Valid")
	require.ErrorIs(t, err, urn.ErrInvalid)
}
