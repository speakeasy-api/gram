package chat

import "strings"

// jsonNULEscape is the only JSON spelling of U+0000 inside a string value.
// Postgres jsonb refuses it (SQLSTATE 22P05) just as text refuses the raw
// byte, so an inline content_raw copy that carries it cannot be stored.
var jsonNULEscape = []byte(`\u` + "0000")

// StripNUL removes every NUL byte from s. Postgres text columns reject NUL
// outright (SQLSTATE 22021: invalid byte sequence for encoding "UTF8":
// 0x00), and imported provider transcripts occasionally carry one from
// pasted binary or terminal output. Dropping the byte, rather than failing
// the write, is what keeps a sync from wedging on that message.
func StripNUL(s string) string {
	return strings.ReplaceAll(s, "\x00", "")
}
