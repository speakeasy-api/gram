package dialect

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestForSpanSelectsNilDialect(t *testing.T) {
	t.Parallel()

	require.IsType(t, NilSpan{}, ForSpan(nil))
}
