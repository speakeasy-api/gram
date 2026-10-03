package assistantidentity_test

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/stretchr/testify/require"
)

func TestIssuerRejectsForceQuery(t *testing.T) {
	t.Parallel()
	_, err := assistantidentity.New("https://platform.example.invalid?", false)
	require.ErrorIs(t, err, assistantidentity.ErrInvalidIdentity)
}

func TestNilIdentityServiceFailsClosed(t *testing.T) {
	t.Parallel()
	var service *assistantidentity.Service
	require.ErrorIs(t, service.BindRootTrigger(t.Context(), nil, "", [16]byte{}, [16]byte{}), assistantidentity.ErrInvalidIdentity)
	require.ErrorIs(t, service.RetargetRootTrigger(t.Context(), nil, "", [16]byte{}, [16]byte{}), assistantidentity.ErrInvalidIdentity)
}

func TestIssuerRejectsEmptyHostnameAndFragmentDelimiter(t *testing.T) {
	t.Parallel()
	for _, origin := range []string{"https://:443", "https://platform.example.invalid#", "https://platform.example.invalid/#"} {
		_, err := assistantidentity.New(origin, false)
		require.ErrorIs(t, err, assistantidentity.ErrInvalidIdentity, origin)
	}
}
