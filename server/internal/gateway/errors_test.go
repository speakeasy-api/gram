package gateway

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestErrNotFound_Wraps(t *testing.T) {
	t.Parallel()

	wrapped := fmt.Errorf("lookup tool: %w", ErrNotFound)
	require.True(t, errors.Is(wrapped, ErrNotFound))
}

func TestErrNotFound_DistinctFromOtherErrors(t *testing.T) {
	t.Parallel()

	other := errors.New("some other error")
	require.False(t, errors.Is(other, ErrNotFound))
}
