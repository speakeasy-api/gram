package slackapproval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/risk/approval"
	"github.com/speakeasy-api/gram/server/internal/risk/approval/repo"
)

const (
	// ActionApprove is the action_id of the Approve once button.
	ActionApprove = "risk_approval_approve"

	// ActionDeny is the action_id of the Deny button.
	ActionDeny = "risk_approval_deny"

	// actionOpen is the action_id of the link button to the dashboard. Slack
	// reports its clicks too; the interactions endpoint ignores them.
	actionOpen = "risk_approval_open"

	// maxArgumentFields bounds the argument fields on a card. Slack allows ten
	// fields in a section; when there are more, the last one says how many.
	maxArgumentFields = 10

	// maxArgumentNameChars and maxArgumentValueChars bound one argument field
	// after escaping, well inside Slack's 2,000-character field limit.
	maxArgumentNameChars  = 100
	maxArgumentValueChars = 300

	// maxReasonChars bounds the agent's reason and an approver's note on a
	// card after escaping and quoting. Both are capped on the way in too; this
	// keeps one card inside Slack's limits however the text reads.
	maxReasonChars = 1500

	// maxNameChars bounds a tool, person, server or policy name on a card.
	maxNameChars = 200
)

// emojiCode matches a Slack emoji code such as :tada:, which renders as an
// emoji in mrkdwn.
var emojiCode = regexp.MustCompile(`:[a-z0-9+'-]+:`)

// italicMarker matches an underscore at a word edge, where Slack reads it as
// the start or end of italics. Underscores inside a word, as in dry_run, stay
// literal.
var italicMarker = regexp.MustCompile(`(^|[^\p{L}\p{N}])_|_($|[^\p{L}\p{N}])`)

// block is the subset of a Slack Block Kit block the approval card uses.
type block struct {
	// Type is the block kind: section, actions or context.
	Type string `json:"type"`

	// BlockID names the block in click payloads.
	BlockID string `json:"block_id,omitempty"`

	// Text is a section's text.
	Text *textObject `json:"text,omitempty"`

	// Fields are a section's two-column fields.
	Fields []textObject `json:"fields,omitempty"`

	// Elements are an actions block's buttons or a context block's texts.
	Elements []element `json:"elements,omitempty"`
}

// textObject is Block Kit text, plain or mrkdwn.
type textObject struct {
	// Type is plain_text or mrkdwn.
	Type string `json:"type"`

	// Text is what Slack shows.
	Text string `json:"text"`
}

// element is a button in an actions block, or a text in a context block.
type element struct {
	// Type is button, plain_text or mrkdwn.
	Type string `json:"type"`

	// Text is a button's label, or a context element's text.
	Text *textObject `json:"text,omitempty"`

	// TextValue is a context element's text. Context elements are text
	// objects themselves, so their text sits at this level.
	TextValue string `json:"-"`

	// ActionID names a button in click payloads.
	ActionID string `json:"action_id,omitempty"`

	// Value is the button's payload, the approval request ID.
	Value string `json:"value,omitempty"`

	// URL makes a link button.
	URL string `json:"url,omitempty"`

	// Style is primary or danger.
	Style string `json:"style,omitempty"`

	// Confirm asks before the click is sent.
	Confirm *confirmDialog `json:"confirm,omitempty"`
}

// MarshalJSON writes a context element as a text object and a button as a
// button.
func (e element) MarshalJSON() ([]byte, error) {
	if e.Type == "mrkdwn" || e.Type == "plain_text" {
		data, err := json.Marshal(textObject{Type: e.Type, Text: e.TextValue})
		if err != nil {
			return nil, fmt.Errorf("marshal context text: %w", err)
		}
		return data, nil
	}
	type button element
	data, err := json.Marshal(button(e))
	if err != nil {
		return nil, fmt.Errorf("marshal button: %w", err)
	}
	return data, nil
}

// confirmDialog is the confirmation Slack shows before sending a click.
type confirmDialog struct {
	// Title heads the dialog.
	Title textObject `json:"title"`

	// Text explains what the click does.
	Text textObject `json:"text"`

	// Confirm labels the button that sends the click.
	Confirm textObject `json:"confirm"`

	// Deny labels the button that closes the dialog.
	Deny textObject `json:"deny"`

	// Style colors the confirm button.
	Style string `json:"style,omitempty"`
}

// card is one message: the notification text and the blocks.
type card struct {
	// Text is the notification and accessibility fallback.
	Text string

	// Blocks are what Slack renders.
	Blocks []block
}

// cardInput is everything a card shows about one request.
type cardInput struct {
	// Approval is the request.
	Approval repo.RiskPolicyApproval

	// Status is the request's status as approvers see it.
	Status approval.Status

	// PolicyName names the policy that held the call.
	PolicyName string

	// RequesterName names who made the call.
	RequesterName string

	// ServerName names the MCP server the tool belongs to. Empty when the
	// request did not record one.
	ServerName string

	// DeciderName names who decided, once someone has.
	DeciderName string

	// DashboardURL opens the request on the Tool approvals tab.
	DashboardURL string
}

// pendingCard asks an approver to decide.
func pendingCard(in cardInput) card {
	blocks := []block{
		mrkdwnSection(":raised_hand: *Approval needed* to call " + code(in.Approval.ToolName, maxNameChars)),
		detailFields(in),
	}
	blocks = append(blocks, detailSections(in)...)
	buttons := []element{
		{Type: "button", Text: plainText("Approve once"), ActionID: ActionApprove, Value: in.Approval.ID.String(), Style: "primary", URL: "", TextValue: "", Confirm: nil},
		{Type: "button", Text: plainText("Deny"), ActionID: ActionDeny, Value: in.Approval.ID.String(), Style: "danger", URL: "", TextValue: "", Confirm: &confirmDialog{
			Title: textObject{Type: "plain_text", Text: "Deny this call?"},
			// Slack shows a confirm dialog's text as plain text, so no mrkdwn here.
			Text:    textObject{Type: "plain_text", Text: fmt.Sprintf("The agent is told the call to %s was denied.", conv.TruncateString(in.Approval.ToolName, maxNameChars))},
			Confirm: textObject{Type: "plain_text", Text: "Deny"},
			Deny:    textObject{Type: "plain_text", Text: "Cancel"},
			Style:   "danger",
		}},
	}
	if in.DashboardURL != "" {
		buttons = append(buttons, element{Type: "button", Text: plainText("Open in Speakeasy"), ActionID: actionOpen, Value: "", URL: in.DashboardURL, Style: "", TextValue: "", Confirm: nil})
	}
	blocks = append(blocks,
		block{Type: "actions", BlockID: "risk_approval", Text: nil, Fields: nil, Elements: buttons},
		contextBlock("Approve once lets this exact call run one time. The reason above was written by the agent."),
	)
	return card{Text: "Approval needed to call " + code(in.Approval.ToolName, maxNameChars), Blocks: blocks}
}

// decidedCard replaces a pending card once the request is decided or lapses.
// It keeps the details for the record and drops the buttons.
func decidedCard(in cardInput) card {
	headline, text := decidedHeadline(in)
	blocks := []block{
		mrkdwnSection(headline),
		detailFields(in),
	}
	blocks = append(blocks, detailSections(in)...)
	if in.DashboardURL != "" {
		blocks = append(blocks, contextBlock(fmt.Sprintf("<%s|Open in Speakeasy>", in.DashboardURL)))
	}
	return card{Text: text, Blocks: blocks}
}

func decidedHeadline(in cardInput) (string, string) {
	// Slack reads the notification text as mrkdwn too, so the name is code
	// there as well.
	tool := code(in.Approval.ToolName, maxNameChars)
	decider := ""
	if in.DeciderName != "" {
		decider = " by " + fit(in.DeciderName, maxNameChars)
	}
	switch in.Status {
	case approval.StatusApproved:
		return fmt.Sprintf(":white_check_mark: *Approved*%s: one call to %s", decider, tool), fmt.Sprintf("Approved: one call to %s", tool)
	case approval.StatusConsumed:
		return fmt.Sprintf(":white_check_mark: *Approved*%s: the call to %s ran", decider, tool), fmt.Sprintf("Approved: the call to %s ran", tool)
	case approval.StatusDenied:
		return fmt.Sprintf(":no_entry: *Denied*%s: the call to %s", decider, tool), fmt.Sprintf("Denied: the call to %s", tool)
	case approval.StatusExpired:
		return fmt.Sprintf(":hourglass: *Expired*: nobody decided on the call to %s in time", tool), fmt.Sprintf("Expired: the call to %s", tool)
	case approval.StatusBlocked, approval.StatusPending:
		return fmt.Sprintf(":raised_hand: *Waiting for a decision* on the call to %s", tool), fmt.Sprintf("Waiting for a decision on the call to %s", tool)
	}
	return fmt.Sprintf("The call to %s is %s", tool, fit(string(in.Status), maxNameChars)), fmt.Sprintf("The call to %s is %s", tool, fit(string(in.Status), maxNameChars))
}

func detailFields(in cardInput) block {
	fields := []textObject{
		{Type: "mrkdwn", Text: "*Requester*\n" + fit(in.RequesterName, maxNameChars)},
	}
	if in.ServerName != "" {
		fields = append(fields, textObject{Type: "mrkdwn", Text: "*Server*\n" + fit(in.ServerName, maxNameChars)})
	}
	fields = append(fields, textObject{Type: "mrkdwn", Text: "*Policy*\n" + fit(in.PolicyName, maxNameChars)})
	if in.Approval.ExpiresAt.Valid {
		fields = append(fields, textObject{Type: "mrkdwn", Text: "*Expires*\n" + slackDate(in.Approval.ExpiresAt.Time)})
	}
	if note := in.Approval.DecisionNote.String; in.Approval.DecisionNote.Valid && note != "" {
		fields = append(fields, textObject{Type: "mrkdwn", Text: "*Note*\n" + fit(note, maxReasonChars)})
	}
	return block{Type: "section", BlockID: "", Text: nil, Fields: fields, Elements: nil}
}

func detailSections(in cardInput) []block {
	blocks := []block{}
	if reason := in.Approval.Reason.String; in.Approval.Reason.Valid && reason != "" {
		blocks = append(blocks, mrkdwnSection("*Reason, from the agent*\n"+quote(reason, maxReasonChars)))
	}
	if args, ok := argumentsSection(in.Approval.ArgumentsPreview); ok {
		blocks = append(blocks, args)
	}
	return blocks
}

// argumentsSection renders the redacted arguments preview as one field per
// argument, sorted by name and bounded so the card stays inside Slack's
// limits. It reports false when there are no arguments to show.
func argumentsSection(raw []byte) (block, bool) {
	var none block
	if len(raw) == 0 {
		return none, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var preview any
	if err := decoder.Decode(&preview); err != nil || preview == nil {
		return none, false
	}
	arguments, ok := preview.(map[string]any)
	if !ok {
		arguments = map[string]any{"value": preview}
	}
	if len(arguments) == 0 {
		return none, false
	}
	names := slices.Sorted(maps.Keys(arguments))
	fields := make([]textObject, 0, min(len(names), maxArgumentFields))
	for i, name := range names {
		if i == maxArgumentFields-1 && len(names) > maxArgumentFields {
			fields = append(fields, textObject{Type: "mrkdwn", Text: fmt.Sprintf("*More*\n%d more arguments, in Speakeasy", len(names)-i)})
			break
		}
		fields = append(fields, textObject{Type: "mrkdwn", Text: argumentName(name) + "\n" + argumentValue(arguments[name])})
	}
	return block{Type: "section", BlockID: "", Text: &textObject{Type: "mrkdwn", Text: "*Arguments*"}, Fields: fields, Elements: nil}, true
}

// argumentValue renders one argument. Plain text reads as text. Everything
// else reads as compact JSON in code: numbers, booleans and null, so the
// string "true" and the boolean true look different, and text that Slack
// would format, so an argument cannot pass for the card's own wording. The
// preview stores nested values as their JSON text, which reads as code as is.
func argumentValue(value any) string {
	text, isText := value.(string)
	if isText && nestedArgument(text) {
		return code(text, maxArgumentValueChars)
	}
	if isText && plainArgument(text) {
		return fit(text, maxArgumentValueChars)
	}
	encoded, ok := compactJSON(value)
	if !ok {
		return "_cannot show this value_"
	}
	return code(encoded, maxArgumentValueChars)
}

// argumentName renders an argument's name as its field's bold title. A name
// Slack would format reads as code instead, so it cannot end the title early
// or add lines that pass for the card's own.
func argumentName(name string) string {
	if strings.TrimSpace(name) != "" && !strings.ContainsAny(name, "*~`\r\n") && !emojiCode.MatchString(name) && !italicMarker.MatchString(name) {
		return "*" + fit(name, maxArgumentNameChars) + "*"
	}
	encoded, ok := compactJSON(name)
	if !ok {
		return "*Argument*"
	}
	return code(encoded, maxArgumentNameChars)
}

// compactJSON encodes value on one line, leaving <, > and & for fit to
// escape.
func compactJSON(value any) (string, bool) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", false
	}
	return strings.TrimSpace(buf.String()), true
}

// plainArgument reports text that Slack shows as written: not blank, on one
// line, with no formatting markers or emoji codes.
func plainArgument(text string) bool {
	return strings.TrimSpace(text) != "" && !strings.ContainsAny(text, "*_~`\r\n") && !emojiCode.MatchString(text)
}

// nestedArgument reports the JSON text the preview stores for a nested value.
func nestedArgument(text string) bool {
	trimmed := strings.TrimSpace(text)
	return (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) && !strings.ContainsAny(text, "\r\n")
}

func mrkdwnSection(text string) block {
	return block{Type: "section", BlockID: "", Text: &textObject{Type: "mrkdwn", Text: text}, Fields: nil, Elements: nil}
}

func contextBlock(text string) block {
	return block{Type: "context", BlockID: "", Text: nil, Fields: nil, Elements: []element{{Type: "mrkdwn", Text: nil, TextValue: text, ActionID: "", Value: "", URL: "", Style: "", Confirm: nil}}}
}

func plainText(text string) *textObject {
	return &textObject{Type: "plain_text", Text: text}
}

// slackDate renders a time in each reader's own timezone, with a UTC fallback
// for clients that cannot.
func slackDate(t time.Time) string {
	return fmt.Sprintf("<!date^%d^{date_short_pretty} at {time}|%s>", t.Unix(), t.UTC().Format("2006-01-02 15:04 UTC"))
}

// quote renders untrusted text as a block quote, so it cannot pass for the
// card's own wording. Like fit, it bounds the result, quote markers included.
func quote(text string, limit int) string {
	return bounded(text, limit, "> ")
}

// code renders text as inline code, bounded like fit. A backtick would end the
// span early, so it reads as a quote mark.
func code(text string, limit int) string {
	return "`" + fit(strings.ReplaceAll(text, "`", "'"), limit) + "`"
}

// fit makes text safe inside mrkdwn and cuts it to at most limit characters,
// plus an ellipsis when cut. Slack reads &, < and > as control characters, so
// they are escaped and links, mentions and dates cannot be smuggled in. The
// bound applies to the escaped text, since escaping can grow a character
// fivefold and Slack's limits apply to what is sent.
func fit(text string, limit int) string {
	return bounded(text, limit, "")
}

// bounded escapes text like fit, starts each line with linePrefix, and stops
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
