package slackdm

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// emojiCode matches a Slack emoji code such as :tada:, which renders as an
// emoji in mrkdwn.
var emojiCode = regexp.MustCompile(`:[a-z0-9_+'-]+:`)

// italicMarker matches an underscore at a word edge, where Slack reads it as
// the start or end of italics. Underscores inside a word, as in dry_run, stay
// literal.
var italicMarker = regexp.MustCompile(`(^|[^\p{L}\p{N}])_|_($|[^\p{L}\p{N}])`)

// Message is one Slack message: the notification text and the blocks.
type Message struct {
	// Text is the notification and accessibility fallback.
	Text string

	// Blocks are what Slack renders.
	Blocks []Block
}

// Block is the subset of a Slack Block Kit block the DM cards use.
type Block struct {
	// Type is the block kind: section, actions or context.
	Type string `json:"type"`

	// BlockID names the block in click payloads.
	BlockID string `json:"block_id,omitempty"`

	// Text is a section's text.
	Text *TextObject `json:"text,omitempty"`

	// Fields are a section's two-column fields.
	Fields []TextObject `json:"fields,omitempty"`

	// Elements are an actions block's buttons or a context block's texts.
	Elements []Element `json:"elements,omitempty"`
}

// TextObject is Block Kit text, plain or mrkdwn.
type TextObject struct {
	// Type is plain_text or mrkdwn.
	Type string `json:"type"`

	// Text is what Slack shows.
	Text string `json:"text"`
}

// Element is a button or static_select in an actions block, or a text in a
// context block.
type Element struct {
	// Type is button, static_select, plain_text or mrkdwn.
	Type string `json:"type"`

	// Text is a button's label. Context elements carry their text in
	// TextValue instead.
	Text *TextObject `json:"text,omitempty"`

	// TextValue is a context element's text. Context elements are text
	// objects themselves, so their text sits at this level.
	TextValue string `json:"-"`

	// ActionID names a button in click payloads.
	ActionID string `json:"action_id,omitempty"`

	// Value is the button's payload, such as the ID of the request it decides.
	Value string `json:"value,omitempty"`

	// URL makes a link button.
	URL string `json:"url,omitempty"`

	// Style is primary or danger.
	Style string `json:"style,omitempty"`

	// Confirm asks before the click is sent.
	Confirm *ConfirmDialog `json:"confirm,omitempty"`

	// Placeholder is a static_select's prompt before anything is chosen.
	Placeholder *TextObject `json:"placeholder,omitempty"`

	// Options are a static_select's choices.
	Options []Option `json:"options,omitempty"`

	// InitialOption is a static_select's choice before anyone picks one.
	InitialOption *Option `json:"initial_option,omitempty"`
}

// Option is one choice in a static_select.
type Option struct {
	// Text is what the menu shows.
	Text TextObject `json:"text"`

	// Value comes back in the click payload as selected_option.value.
	Value string `json:"value"`
}

// MarshalJSON writes a context element as a text object and a button as a
// button.
func (e Element) MarshalJSON() ([]byte, error) {
	if e.Type == "mrkdwn" || e.Type == "plain_text" {
		data, err := json.Marshal(TextObject{Type: e.Type, Text: e.TextValue})
		if err != nil {
			return nil, fmt.Errorf("marshal context text: %w", err)
		}
		return data, nil
	}
	type button Element
	data, err := json.Marshal(button(e))
	if err != nil {
		return nil, fmt.Errorf("marshal button: %w", err)
	}
	return data, nil
}

// ConfirmDialog is the confirmation Slack shows before sending a click.
type ConfirmDialog struct {
	// Title heads the dialog.
	Title TextObject `json:"title"`

	// Text explains what the click does.
	Text TextObject `json:"text"`

	// Confirm labels the button that sends the click.
	Confirm TextObject `json:"confirm"`

	// Deny labels the button that closes the dialog.
	Deny TextObject `json:"deny"`

	// Style colors the confirm button.
	Style string `json:"style,omitempty"`
}

// MrkdwnSection is a section block with mrkdwn text.
func MrkdwnSection(text string) Block {
	return Block{Type: "section", BlockID: "", Text: &TextObject{Type: "mrkdwn", Text: text}, Fields: nil, Elements: nil}
}

// ContextBlock is a context block with one mrkdwn text.
func ContextBlock(text string) Block {
	return Block{Type: "context", BlockID: "", Text: nil, Fields: nil, Elements: []Element{{Type: "mrkdwn", Text: nil, TextValue: text, ActionID: "", Value: "", URL: "", Style: "", Confirm: nil, Placeholder: nil, Options: nil, InitialOption: nil}}}
}

// PlainText is a plain_text text object, as buttons and dialogs take.
func PlainText(text string) *TextObject {
	return &TextObject{Type: "plain_text", Text: text}
}

// Date renders a time in each reader's own timezone, with a UTC fallback for
// clients that cannot.
func Date(t time.Time) string {
	return fmt.Sprintf("<!date^%d^{date_short_pretty} at {time}|%s>", t.Unix(), t.UTC().Format("2006-01-02 15:04 UTC"))
}

// ShowsAsWritten reports text that Slack shows as written: not blank, on one
// line, with no formatting markers or emoji codes.
func ShowsAsWritten(text string) bool {
	return strings.TrimSpace(text) != "" && !strings.ContainsAny(text, "*~`\r\n") && !emojiCode.MatchString(text) && !italicMarker.MatchString(text)
}

// Quote renders untrusted text as a block quote, so it cannot pass for the
// card's own wording. Like Fit, it bounds the result, quote markers included.
func Quote(text string, limit int) string {
	return bounded(text, limit, "> ")
}

// Code renders text as inline code, bounded like Fit. A backtick would end the
// span early, so it reads as a quote mark.
func Code(text string, limit int) string {
	return "`" + Fit(strings.ReplaceAll(text, "`", "'"), limit) + "`"
}

// Fit escapes text for mrkdwn and cuts it to at most limit characters, plus an
// ellipsis when cut. Slack reads &, < and > as control characters, so they are
// escaped and links, mentions and dates cannot be smuggled in. Formatting
// markers and emoji codes still render; callers that must rule those out check
// the text with ShowsAsWritten first. The
// bound applies to the escaped text, since escaping can grow a character
// fivefold and Slack's limits apply to what is sent.
func Fit(text string, limit int) string {
	return bounded(text, limit, "")
}

// bounded escapes text like Fit, starts each line with linePrefix, and stops
// before the result passes limit characters, ending in an ellipsis when it
// does.
func bounded(text string, limit int, linePrefix string) string {
	var out strings.Builder
	out.WriteString(linePrefix)
	used := utf8.RuneCountInString(linePrefix)
	for _, r := range text {
		var piece string
		switch r {
		case '&':
			piece = "&amp;"
		case '<':
			piece = "&lt;"
		case '>':
			piece = "&gt;"
		case '\n':
			piece = "\n" + linePrefix
		default:
			piece = string(r)
		}
		size := utf8.RuneCountInString(piece)
		if used+size > limit {
			out.WriteString("…")
			return out.String()
		}
		out.WriteString(piece)
		used += size
	}
	return out.String()
}
