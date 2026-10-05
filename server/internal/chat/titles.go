package chat

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

// Stand-in titles a capture path seeds a chat with until the title generator
// names it. A path that seeds any other string opts its chats out of generation.
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

// placeholderTitles is the vocabulary IsPlaceholderTitle recognizes.
var placeholderTitles = []string{
	"",
	DefaultChatTitle,
	DefaultClaudeChatTitle,
	DefaultCoworkChatTitle,
	DefaultClaudeAmbiguous,
	DefaultCursorChatTitle,
	DefaultCodexChatTitle,
	DefaultInferenceChatTitle,
}

// MaxDerivedTitleRunes bounds a title derived from message content. Titles
// render in dense lists, so a long first prompt is cut rather than allowed to
// dominate the row.
const MaxDerivedTitleRunes = 80

// IsPlaceholderTitle reports whether a title is one of the fixed stand-ins a
// capture path seeds a chat with, rather than a name that carries meaning.
func IsPlaceholderTitle(title string) bool {
	return slices.Contains(placeholderTitles, strings.TrimSpace(title))
}

// DerivedTitle renders the stand-in title a capture path stores for a chat it
// opens with content in hand. IsDerivedTitle recognizes the result, so every
// path that derives a title must go through this function.
func DerivedTitle(content string) string {
	return conv.TruncateString(strings.TrimSpace(content), MaxDerivedTitleRunes)
}

// IsDerivedTitle reports whether title is the stand-in DerivedTitle would
// produce for any of the supplied message bodies. A generated or source-chosen
// title that happens to equal a message's opening is indistinguishable from a
// stand-in and is regenerated.
func IsDerivedTitle(title string, contents ...string) bool {
	if title == "" {
		return false
	}
	titleRunes := utf8.RuneCountInString(title)
	if titleRunes > MaxDerivedTitleRunes {
		return false
	}
	// A stand-in is its source trimmed, cut to MaxDerivedTitleRunes. Checking
	// the prefix avoids decoding every multi-kilobyte message into runes.
	truncated := titleRunes == MaxDerivedTitleRunes
	for _, content := range contents {
		content = strings.TrimSpace(content)
		if content == title || (truncated && strings.HasPrefix(content, title)) {
			return true
		}
	}
	return false
}
