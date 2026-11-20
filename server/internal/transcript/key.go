package transcript

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"regexp"
	"strings"
)

// ContentKeyVersion changes whenever ContentKey or its normalization
// changes. Rows keep the version their key was made with, so old keys can be
// recomputed.
const ContentKeyVersion = 1

// systemReminder matches the blocks a client injects into a prompt or a
// tool result before it reaches the model, with or without attributes on the
// opening tag. Inference frames carry them; transcripts and hooks do not, so
// they must not change the key.
var systemReminder = regexp.MustCompile(`(?s)<system-reminder(?:\s[^>]*)?>.*?</system-reminder>`)

// ContentKey is a key that Speakeasy makes from what an entry says. It is
// not unique by itself: two "yes" prompts have the same key, and only their
// parent or their natural IDs tell them apart.
func ContentKey(e Entry) []byte {
	h := sha256.New()
	write := func(parts ...string) {
		for _, p := range parts {
			h.Write([]byte(p))
			h.Write([]byte{0})
		}
	}
	write(string(e.Role))
	switch {
	case e.Call != nil:
		write("call", e.Call.Name, canonicalJSON(e.Call.Input))
	case e.Result != nil:
		write("result", e.Result.CallID, normalize(e.Result.Text, true))
	default:
		// Clients inject reminders into the input side only. Model output
		// keeps its text as written.
		write("text", normalize(e.Text, e.Role == RoleUser))
	}
	return h.Sum(nil)
}

// PathKey keys the path from a root to an entry: the parent's path key
// followed by the entry's content key. It finds where a full trajectory
// leaves the stored tree in one lookup. It is an index, never an identity.
func PathKey(parent, content []byte) []byte {
	h := sha256.New()
	h.Write(parent)
	h.Write(content)
	return h.Sum(nil)
}

func normalize(s string, stripReminders bool) string {
	if stripReminders {
		s = systemReminder.ReplaceAllString(s, "")
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.TrimSpace(s)
}

// canonicalJSON re-encodes a tool input so formatting and key order do not
// change the key. Input that is not one valid JSON value is used as written,
// without outer space.
func canonicalJSON(raw []byte) string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil || dec.More() {
		return strings.TrimSpace(string(raw))
	}
	out, err := json.Marshal(v)
	if err != nil {
		return strings.TrimSpace(string(raw))
	}
	return string(out)
}
