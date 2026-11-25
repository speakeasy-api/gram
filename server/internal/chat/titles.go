package chat

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/chat/chattitle"
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
const MaxDerivedTitleRunes = chattitle.MaxRunes

// IsPlaceholderTitle reports whether a title is one of the fixed stand-ins a
// capture path seeds a chat with, rather than a name that carries meaning.
func IsPlaceholderTitle(title string) bool {
	return slices.Contains(placeholderTitles, strings.TrimSpace(title))
}

// DerivedTitle renders the stand-in title a capture path stores for a chat it
// opens with content in hand: the human text after any leading harness
// envelopes, trimmed and cut to MaxDerivedTitleRunes. It returns "" when the
// content is only envelopes, so the caller can seed its surface placeholder
// instead. IsDerivedTitle recognizes the result, so every path that derives a
// title must go through this function.
func DerivedTitle(content string) string {
	return conv.TruncateString(strings.TrimSpace(StripLeadingEnvelopes(content)), MaxDerivedTitleRunes)
}

// IsDerivedTitle reports whether title is the stand-in DerivedTitle would
// produce for any of the supplied message bodies. A generated or source-chosen
// title that happens to equal a message's opening is indistinguishable from a
// stand-in and is regenerated. A stored stand-in can hold the raw opening,
// envelope included, so both the raw and the stripped content are matched.
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
	matches := func(content string) bool {
		content = strings.TrimSpace(content)
		return content == title || (truncated && strings.HasPrefix(content, title))
	}
	for _, content := range contents {
		// Both forms a stand-in comes from are substrings of the content, so
		// this exact check spares the envelope regex on every unrelated
		// (often multi-kilobyte) message.
		if !strings.Contains(content, title) {
			continue
		}
		if matches(content) || matches(StripLeadingEnvelopes(content)) {
			return true
		}
	}
	return false
}

// displayTitlePtr is chattitle.Display for a nullable title column, which
// stays nil when the column is NULL.
func displayTitlePtr(title pgtype.Text, manuallySet bool) *string {
	if !title.Valid {
		return nil
	}
	display := chattitle.Display(title.String, manuallySet)
	return &display
}

// CronJobTitle is chattitle.CronJobTitle, for capture paths that already
// derive titles through this package.
func CronJobTitle(content string) (string, bool) {
	return chattitle.CronJobTitle(content)
}
