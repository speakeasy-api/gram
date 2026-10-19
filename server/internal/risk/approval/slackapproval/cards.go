package slackapproval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

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

	// maxArgumentsChars bounds the arguments shown on a card. Slack caps a
	// section's text at 3,000 characters, and the card keeps room for the
	// code fence and heading.
	maxArgumentsChars = 2500

	// maxReasonChars bounds the agent's reason on a card. Request reasons are
	// already capped on the way in; this keeps one card inside Slack's limits
	// even if that cap grows.
	maxReasonChars = 1500
)

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
		mrkdwnSection(fmt.Sprintf(":raised_hand: *Approval needed* to call `%s`", escape(in.Approval.ToolName))),
		detailFields(in),
	}
	blocks = append(blocks, detailSections(in)...)
	buttons := []element{
		{Type: "button", Text: plainText("Approve once"), ActionID: ActionApprove, Value: in.Approval.ID.String(), Style: "primary", URL: "", TextValue: "", Confirm: nil},
		{Type: "button", Text: plainText("Deny"), ActionID: ActionDeny, Value: in.Approval.ID.String(), Style: "danger", URL: "", TextValue: "", Confirm: &confirmDialog{
			Title: textObject{Type: "plain_text", Text: "Deny this call?"},
			// Slack shows a confirm dialog's text as plain text, so no mrkdwn here.
			Text:    textObject{Type: "plain_text", Text: fmt.Sprintf("The agent is told the call to %s was denied.", in.Approval.ToolName)},
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
	return card{Text: fmt.Sprintf("Approval needed to call %s", in.Approval.ToolName), Blocks: blocks}
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
	tool := escape(in.Approval.ToolName)
	decider := ""
	if in.DeciderName != "" {
		decider = " by " + escape(in.DeciderName)
	}
	switch in.Status {
	case approval.StatusApproved:
		return fmt.Sprintf(":white_check_mark: *Approved*%s: one call to `%s`", decider, tool), fmt.Sprintf("Approved: one call to %s", in.Approval.ToolName)
	case approval.StatusConsumed:
		return fmt.Sprintf(":white_check_mark: *Approved*%s: the call to `%s` ran", decider, tool), fmt.Sprintf("Approved: the call to %s ran", in.Approval.ToolName)
	case approval.StatusDenied:
		return fmt.Sprintf(":no_entry: *Denied*%s: the call to `%s`", decider, tool), fmt.Sprintf("Denied: the call to %s", in.Approval.ToolName)
	case approval.StatusExpired:
		return fmt.Sprintf(":hourglass: *Expired*: nobody decided on the call to `%s` in time", tool), fmt.Sprintf("Expired: the call to %s", in.Approval.ToolName)
	case approval.StatusBlocked, approval.StatusPending:
		return fmt.Sprintf(":raised_hand: *Waiting for a decision* on the call to `%s`", tool), fmt.Sprintf("Waiting for a decision on the call to %s", in.Approval.ToolName)
	}
	return fmt.Sprintf("The call to `%s` is %s", tool, escape(string(in.Status))), fmt.Sprintf("The call to %s is %s", in.Approval.ToolName, in.Status)
}

func detailFields(in cardInput) block {
	fields := []textObject{
		{Type: "mrkdwn", Text: "*Requester*\n" + escape(in.RequesterName)},
	}
	if in.ServerName != "" {
		fields = append(fields, textObject{Type: "mrkdwn", Text: "*Server*\n" + escape(in.ServerName)})
	}
	fields = append(fields, textObject{Type: "mrkdwn", Text: "*Policy*\n" + escape(in.PolicyName)})
	if in.Approval.ExpiresAt.Valid {
		fields = append(fields, textObject{Type: "mrkdwn", Text: "*Expires*\n" + slackDate(in.Approval.ExpiresAt.Time)})
	}
	if note := in.Approval.DecisionNote.String; in.Approval.DecisionNote.Valid && note != "" {
		fields = append(fields, textObject{Type: "mrkdwn", Text: "*Note*\n" + escape(truncate(note, maxReasonChars))})
	}
	return block{Type: "section", BlockID: "", Text: nil, Fields: fields, Elements: nil}
}

func detailSections(in cardInput) []block {
	blocks := []block{}
	if reason := in.Approval.Reason.String; in.Approval.Reason.Valid && reason != "" {
		blocks = append(blocks, mrkdwnSection("*Reason, from the agent*\n"+quote(truncate(reason, maxReasonChars))))
	}
	if args := formatArguments(in.Approval.ArgumentsPreview); args != "" {
		blocks = append(blocks, mrkdwnSection("*Arguments*\n```"+args+"```"))
	}
	return blocks
}

// formatArguments renders the redacted arguments preview as indented JSON for
// a code block, bounded so the card stays inside Slack's limits.
func formatArguments(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var preview any
	if err := decoder.Decode(&preview); err != nil || preview == nil {
		return ""
	}
	if object, ok := preview.(map[string]any); ok && len(object) == 0 {
		return ""
	}
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(preview); err != nil {
		return ""
	}
	// A code fence inside the arguments would end the block early.
	text := strings.ReplaceAll(strings.TrimSpace(buf.String()), "```", "'''")
	return escape(truncate(text, maxArgumentsChars))
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
// card's own wording.
func quote(text string) string {
	lines := strings.Split(escape(text), "\n")
	for i, line := range lines {
		lines[i] = "> " + line
	}
	return strings.Join(lines, "\n")
}

// escape makes text safe inside mrkdwn: Slack reads &, < and > as control
// characters, so links, mentions and dates cannot be smuggled in.
func escape(text string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(text)
}

func truncate(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit]) + "…"
}
