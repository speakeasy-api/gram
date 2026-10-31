package memorydigest

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	// DefaultRecallBudgetTokens bounds the recall block. The RFC's target is
	// a year of team history in under 8k tokens.
	DefaultRecallBudgetTokens = 8_000

	// asciiRunesPerToken estimates tokens for ASCII text. English prose with
	// identifiers averages about 4 characters per token for current
	// tokenizers. Other runes (CJK, emoji, accented text) are counted as one
	// token each, which overestimates them, so the block stays under budget
	// whatever the language.
	asciiRunesPerToken = 4

	// maxChatSnippetRunes caps a chat summary in the recall block. Only the
	// first paragraph is used, which the summary prompt requires to stand
	// alone; the cap guards against a summary that ignores that.
	maxChatSnippetRunes = 600
)

// Digest is a stored digest as the recall block and zoom see it.
type Digest struct {
	// ID is the digest's row id, cited as a digest source.
	ID uuid.UUID

	// Level is the digest's granularity.
	Level Level

	// PeriodStart is the UTC start of the period the digest covers.
	PeriodStart time.Time

	// Content is the masked digest text.
	Content string

	// ChildDigestIDs lists the digests a week or month digest folded.
	ChildDigestIDs []uuid.UUID

	// ChatIDs lists the chats a day digest folded.
	ChatIDs []uuid.UUID
}

// ChatNote is a chat whose masked summary is not yet folded into a digest.
type ChatNote struct {
	// ID is the chat id, cited as a chat source.
	ID uuid.UUID

	// SummarizedAt is when the masked summary was written.
	SummarizedAt time.Time

	// Summary is the masked summary.
	Summary string
}

// Entry is one section of the recall block.
type Entry struct {
	// Kind is SourceDigest or SourceChat.
	Kind SourceKind

	// ID is the digest or chat id to cite or zoom into.
	ID uuid.UUID

	// Heading names the period or chat, for example "October 2026 (month digest)".
	Heading string

	// At orders entries chronologically.
	At time.Time

	// Text is the masked digest content or chat snippet.
	Text string
}

// Uncovered picks the recall entries for a project: every month digest, then
// the week digests no month digest folded, the day digests no week digest
// folded, and the chats no day digest folded. Older history therefore shows
// at its most compressed level, and nothing appears twice. Entries come back
// oldest first.
func Uncovered(digests []Digest, chats []ChatNote) []Entry {
	covered := map[uuid.UUID]bool{}
	for _, d := range digests {
		for _, id := range d.ChildDigestIDs {
			covered[id] = true
		}
		for _, id := range d.ChatIDs {
			covered[id] = true
		}
	}

	// Covered digests still mark their own children covered above, so a
	// month hides its weeks and those weeks' days alike.
	byLevel := func(l Level) []Digest {
		var out []Digest
		for _, d := range digests {
			if d.Level == l {
				out = append(out, d)
			}
		}
		return out
	}

	var entries []Entry
	for _, l := range []Level{LevelMonth, LevelWeek, LevelDay} {
		for _, d := range byLevel(l) {
			if covered[d.ID] {
				continue
			}
			entries = append(entries, Entry{
				Kind:    SourceDigest,
				ID:      d.ID,
				Heading: fmt.Sprintf("%s (%s digest)", l.Label(d.PeriodStart), l),
				At:      d.PeriodStart,
				Text:    strings.TrimSpace(d.Content),
			})
		}
	}
	for _, c := range chats {
		if covered[c.ID] {
			continue
		}
		// Chat titles are generated from the raw transcript and are not
		// masked at rest, so the heading carries only the date.
		entries = append(entries, Entry{
			Kind:    SourceChat,
			ID:      c.ID,
			Heading: "Chat " + c.SummarizedAt.UTC().Format(time.DateOnly),
			At:      c.SummarizedAt,
			Text:    firstParagraph(c.Summary, maxChatSnippetRunes),
		})
	}

	slices.SortStableFunc(entries, func(a, b Entry) int { return a.At.Compare(b.At) })
	return entries
}

// Budget keeps the newest entries whose estimated size fits budgetTokens and
// returns them oldest first, with the number of older entries dropped.
// Entries are ordered by At first, so callers need not pre-sort them.
// Recent history is the most detailed and the most often needed, so the
// oldest entries go first when a project outgrows the budget.
func Budget(entries []Entry, budgetTokens int) (kept []Entry, dropped int) {
	entries = slices.Clone(entries)
	slices.SortStableFunc(entries, func(a, b Entry) int { return a.At.Compare(b.At) })
	cost := func(e Entry) int { return EstimateTokens(renderEntry(e)) + EstimateTokens(entrySeparator) }
	used := 0
	start := len(entries)
	for i := len(entries) - 1; i >= 0; i-- {
		if used+cost(entries[i]) > budgetTokens {
			break
		}
		used += cost(entries[i])
		start = i
	}
	// Render adds an omission notice only when something was dropped; make
	// room for it by dropping more of the oldest kept entries.
	for start > 0 && start < len(entries) && used+EstimateTokens(omittedNotice(start)) > budgetTokens {
		used -= cost(entries[start])
		start++
	}
	return entries[start:], start
}

// EstimateTokens approximates the token count of s, erring high for
// non-ASCII text.
func EstimateTokens(s string) int {
	ascii, other := 0, 0
	for _, r := range s {
		if r < utf8.RuneSelf {
			ascii++
		} else {
			other++
		}
	}
	return (ascii+asciiRunesPerToken-1)/asciiRunesPerToken + other
}

// firstParagraph returns the first non-empty paragraph of s, cut to maxRunes.
func firstParagraph(s string, maxRunes int) string {
	for p := range strings.SplitSeq(strings.TrimSpace(s), "\n\n") {
		if p = strings.TrimSpace(p); p != "" {
			s = p
			break
		}
	}
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	return string([]rune(s)[:maxRunes-1]) + "…"
}

// Citation formats a source the way agents cite it: [digest:<id>],
// [chat:<id>] or [memo:<id>].
func Citation(kind SourceKind, id uuid.UUID) string {
	return "[" + string(kind) + ":" + id.String() + "]"
}

// Render writes entries as one text block, oldest first, each under a
// heading that carries its citation. dropped is the number of older
// entries Budget left out, which the block states so an agent knows to
// search or zoom for older history.
func Render(entries []Entry, dropped int) string {
	var b strings.Builder
	if dropped > 0 {
		b.WriteString(omittedNotice(dropped))
	}
	for i, e := range entries {
		if i > 0 {
			b.WriteString(entrySeparator)
		}
		b.WriteString(renderEntry(e))
	}
	return b.String()
}

// entrySeparator sits between rendered recall entries.
const entrySeparator = "\n\n"

func renderEntry(e Entry) string {
	return "## " + e.Heading + " " + Citation(e.Kind, e.ID) + "\n" + e.Text
}

func omittedNotice(dropped int) string {
	return fmt.Sprintf("(%d older entries omitted to fit the budget; use search_team_memory for older history.)\n\n", dropped)
}
