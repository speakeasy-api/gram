package intentpolicy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStripJSONNUL(t *testing.T) {
	t.Parallel()

	require.JSONEq(t, `{"command":"cat ab"}`, stripJSONNUL(`{"command":"cat a\u0000b"}`))
	require.JSONEq(t, `{"command":"cat ab"}`, stripJSONNUL("{\"command\":\"cat a\x00b\"}"))
	// An escaped backslash followed by u0000 is literal text, not a NUL.
	literal := `{"s":"a\\u0000b"}`
	require.Equal(t, literal, stripJSONNUL(literal), "must be left byte-identical")
	require.Equal(t, "plain \\u0000 text", stripNUL("plain \\u0000 text"))
}
