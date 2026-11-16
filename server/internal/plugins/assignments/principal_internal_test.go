package assignments

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestNormalizePrincipalRejectsMachinePrincipals(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"api_key:" + uuid.NewString(), "agent:" + uuid.NewString(), "system:issuer-metadata-refresh"} {
		_, err := normalizePrincipal(t.Context(), nil, "org", raw)
		require.ErrorIs(t, err, ErrInvalid, raw)
	}
}
