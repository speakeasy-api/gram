package dialect

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestForLogHandlesNilRecord(t *testing.T) {
	t.Parallel()

	require.IsType(t, NilLog{}, ForLog(nil))
}
