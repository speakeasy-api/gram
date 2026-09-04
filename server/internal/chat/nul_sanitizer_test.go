package chat

import (
	"encoding/json"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

var fffd = string(utf8.RuneError)

func TestReplaceNUL_Untouched(t *testing.T) {
	t.Parallel()

	require.Equal(t, "plain text", ReplaceNUL("plain text"))
	require.Empty(t, ReplaceNUL(""))
}

func TestReplaceNUL_ReplacesEveryNUL(t *testing.T) {
	t.Parallel()

	require.Equal(t, "a"+fffd+"b"+fffd, ReplaceNUL("a\x00b\x00"))
}

func TestReplaceNULInJSON_Untouched(t *testing.T) {
	t.Parallel()

	in := []byte(`[{"type":"text","text":"hello"},{"n":12345678901234567890}]`)
	require.Equal(t, in, replaceNULInJSON(in))
}

func TestReplaceNULInJSON_EscapedBackslashLiteralUntouched(t *testing.T) {
	t.Parallel()

	// A backslash followed by "u0000" in the decoded text is ordinary
	// characters, not a NUL; the JSON form carries a doubled backslash.
	in, err := json.Marshal("literal " + `\u` + "0000 text")
	require.NoError(t, err)
	require.Equal(t, in, replaceNULInJSON(in))
}

func TestReplaceNULInJSON_StringEscapeReplaced(t *testing.T) {
	t.Parallel()

	in := []byte(`"before` + `\u` + `0000after"`)
	require.JSONEq(t, `"before`+fffd+`after"`, string(replaceNULInJSON(in)))
}

func TestReplaceNULInJSON_NestedValuesAndKeysReplacedNumbersPreserved(t *testing.T) {
	t.Parallel()

	in := []byte(`[{"type":"text","text":"a` + `\u` + `0000b"},{"k` + `\u` + `0000":1.50,"n":12345678901234567890}]`)
	out := replaceNULInJSON(in)
	require.JSONEq(t, `[{"type":"text","text":"a`+fffd+`b"},{"k`+fffd+`":1.50,"n":12345678901234567890}]`, string(out))
	require.NotContains(t, string(out), string(jsonNULEscape))
}

func TestReplaceNULInJSON_InvalidJSONWithEscapePassesThrough(t *testing.T) {
	t.Parallel()

	in := []byte(`[` + `\u` + `0000`)
	require.Equal(t, in, replaceNULInJSON(in))
}

func TestReplaceNULInJSON_InvalidJSONWithRawNULStillSubstituted(t *testing.T) {
	t.Parallel()

	out := replaceNULInJSON([]byte("[not json \x00"))
	require.Equal(t, "[not json "+fffd, string(out))
}
