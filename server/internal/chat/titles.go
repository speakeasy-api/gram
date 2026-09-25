package chat

import "strings"

// Every capture path seeds a new chat with a stand-in title so the row is
// displayable before the async title generator has produced a real one. The
// generator only replaces a title it recognizes as a stand-in, so a path that
// invents its own string here silently opts its chats out of title generation
// for good — add the constant to this block and to IsPlaceholderTitle instead.
const (
	// DefaultChatTitle is the surface-agnostic placeholder used by chats the
	// completions proxy and the assistants runtime create.
	DefaultChatTitle = "New Chat"
	// DefaultClaudeChatTitle covers the Claude Code CLI and the desktop app.
	DefaultClaudeChatTitle = "Claude Code Session"
	// DefaultCoworkChatTitle covers Claude Cowork sessions.
	DefaultCoworkChatTitle = "Cowork Session"
	// DefaultClaudeAmbiguous is used when nothing on file yet identifies which
	// Claude surface a session came from.
	DefaultClaudeAmbiguous = "Claude Session"
	// DefaultCursorChatTitle covers Cursor sessions.
	DefaultCursorChatTitle = "Cursor Session"
	// DefaultCodexChatTitle covers Codex sessions.
	DefaultCodexChatTitle = "Codex Session"
	// DefaultInferenceChatTitle covers conversations archived from the
	// Anthropic inference hook.
	DefaultInferenceChatTitle = "Claude inference conversation"
)

// maxDerivedTitleRunes bounds a title derived from message content. Titles are
// rendered in dense lists (sessions, chat logs, risk events), so a long first
// prompt is cut rather than allowed to dominate the row.
const maxDerivedTitleRunes = 80

// IsPlaceholderTitle reports whether a title is one of the fixed stand-ins a
// capture path seeds a chat with, rather than a name that carries meaning.
func IsPlaceholderTitle(title string) bool {
	switch strings.TrimSpace(title) {
	case "",
		DefaultChatTitle,
		DefaultClaudeChatTitle,
		DefaultCoworkChatTitle,
		DefaultClaudeAmbiguous,
		DefaultCursorChatTitle,
		DefaultCodexChatTitle,
		DefaultInferenceChatTitle:
		return true
	default:
		return false
	}
}

// DerivedTitle renders the stand-in title a capture path stores for a chat it
// opens with real content in hand — the hook ingest path titles a session
// after the message that started it, which reads better than a generic
// placeholder while the generator is still working.
//
// Title generation recognizes these by re-deriving them from the chat's own
// messages, which is how a derived stand-in is told apart from a title a
// source deliberately chose (an imported conversation name, a channel label).
// Both sides must therefore derive titles through this one function.
func DerivedTitle(content string) string {
	title := strings.TrimSpace(content)
	runes := []rune(title)
	if len(runes) <= maxDerivedTitleRunes {
		return title
	}
	return string(runes[:maxDerivedTitleRunes])
}

// IsDerivedTitle reports whether title is the stand-in DerivedTitle would
// produce for any of the supplied message bodies.
func IsDerivedTitle(title string, contents ...string) bool {
	if strings.TrimSpace(title) == "" {
		return false
	}
	for _, content := range contents {
		if DerivedTitle(content) == title {
			return true
		}
	}
	return false
}
