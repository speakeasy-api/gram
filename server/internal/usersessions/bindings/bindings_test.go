package bindings

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestValidateAndLock_ZeroIssuerIsNotFound(t *testing.T) {
	t.Parallel()

	_, err := ValidateAndLock(t.Context(), nil, uuid.Nil, uuid.New(), "organization")
	require.ErrorIs(t, err, ErrNotFound)
}
