package oautherr

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsRFC6749TokenEndpointCode(t *testing.T) {
	t.Parallel()

	for _, code := range []string{
		CodeInvalidRequest,
		CodeInvalidClient,
		CodeInvalidGrant,
		CodeUnauthorizedClient,
		CodeUnsupportedGrantType,
		CodeInvalidScope,
	} {
		require.True(t, IsRFC6749TokenEndpointCode(code), code)
	}

	for _, code := range []string{
		"",
		"Invalid_Grant",
		"invalid_grant ",
		CodeAccessDenied,
		CodeServerError,
		CodeTemporarilyUnavailable,
		CodeInvalidToken,
		CodeUnsupportedTokenType,
		CodeExpiredToken,
		CodeInvalidTarget,
		CodeInvalidDPoPProof,
	} {
		require.False(t, IsRFC6749TokenEndpointCode(code), code)
	}
}
