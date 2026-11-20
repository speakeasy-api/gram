package slackapproval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/risk/approval"
	"github.com/speakeasy-api/gram/server/internal/risk/approval/repo"
	"github.com/speakeasy-api/gram/server/internal/slackdm"
)

const (
	// ActionPrefix starts the action ID of every button on an approval
	// request DM. Approvers already have DMs with these IDs, so they stay as
	// they are.
	ActionPrefix = "risk_approval_"

	// ActionApprove is the action_id of the Approve once button.
	ActionApprove = ActionPrefix + "approve"

	// ActionDeny is the action_id of the Deny button.
	ActionDeny = ActionPrefix + "deny"

	// actionOpen is the action_id of the link button to the dashboard. Slack
	// reports its clicks too; the interactions endpoint ignores them.
	actionOpen = ActionPrefix + "open"

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

	// previewOmittedMark makes up the key of the entry the arguments preview
	// adds when it leaves top-level arguments out: one mark, or more when an
	// argument already has that name. previewOmittedNote is its value.
	previewOmittedMark = "…"
	previewOmittedNote = "arguments not shown: %d"
)

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
func pendingCard(in cardInput) slackdm.Message {
	tool := slackdm.Code(in.Approval.ToolName, maxNameChars)
	blocks := []slackdm.Block{
		slackdm.MrkdwnSection(":raised_hand: *Approval needed* to call " + tool),
		detailFields(in),
	}
	blocks = append(blocks, detailSections(in)...)
	buttons := []slackdm.Element{
		{Type: "button", Text: slackdm.PlainText("Approve once"), ActionID: ActionApprove, Value: in.Approval.ID.String(), Style: "primary", URL: "", TextValue: "", Confirm: nil, Placeholder: nil, Options: nil, InitialOption: nil},
		{Type: "button", Text: slackdm.PlainText("Deny"), ActionID: ActionDeny, Value: in.Approval.ID.String(), Style: "danger", URL: "", TextValue: "", Confirm: &slackdm.ConfirmDialog{
			Title: slackdm.TextObject{Type: "plain_text", Text: "Deny this call?"},
			// Slack shows a confirm dialog's text as plain text, so no mrkdwn here.
			Text:    slackdm.TextObject{Type: "plain_text", Text: fmt.Sprintf("The agent is told the call to %s was denied.", conv.TruncateString(in.Approval.ToolName, maxNameChars))},
			Confirm: slackdm.TextObject{Type: "plain_text", Text: "Deny"},
			Deny:    slackdm.TextObject{Type: "plain_text", Text: "Cancel"},
			Style:   "danger",
		}, Placeholder: nil, Options: nil, InitialOption: nil},
	}
	if in.DashboardURL != "" {
		buttons = append(buttons, slackdm.Element{Type: "button", Text: slackdm.PlainText("Open in Speakeasy"), ActionID: actionOpen, Value: "", URL: in.DashboardURL, Style: "", TextValue: "", Confirm: nil, Placeholder: nil, Options: nil, InitialOption: nil})
	}
	blocks = append(blocks,
		slackdm.Block{Type: "actions", BlockID: "risk_approval", Text: nil, Fields: nil, Elements: buttons},
		slackdm.ContextBlock("Approve once lets this exact call run one time. The reason above was written by the agent."),
	)
	return slackdm.Message{Text: "Approval needed to call " + tool, Blocks: blocks}
}

// decidedCard replaces a pending card once the request is decided or lapses.
// It keeps the details for the record and drops the buttons.
func decidedCard(in cardInput) slackdm.Message {
	headline, text := decidedHeadline(in)
	blocks := []slackdm.Block{
		slackdm.MrkdwnSection(headline),
		detailFields(in),
	}
	blocks = append(blocks, detailSections(in)...)
	if in.DashboardURL != "" {
		blocks = append(blocks, slackdm.ContextBlock(fmt.Sprintf("<%s|Open in Speakeasy>", in.DashboardURL)))
	}
	return slackdm.Message{Text: text, Blocks: blocks}
}

func decidedHeadline(in cardInput) (string, string) {
	// Slack reads the notification text as mrkdwn too, so the name is code
	// there as well.
	tool := slackdm.Code(in.Approval.ToolName, maxNameChars)
	decider := ""
	if in.DeciderName != "" {
		decider = " by " + label(in.DeciderName)
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
	other := fmt.Sprintf("The call to %s is %s", tool, slackdm.Fit(string(in.Status), maxNameChars))
	return other, other
}

func detailFields(in cardInput) slackdm.Block {
	fields := []slackdm.TextObject{
		{Type: "mrkdwn", Text: "*Requester*\n" + label(in.RequesterName)},
	}
	if in.ServerName != "" {
		fields = append(fields, slackdm.TextObject{Type: "mrkdwn", Text: "*Server*\n" + label(in.ServerName)})
	}
	fields = append(fields, slackdm.TextObject{Type: "mrkdwn", Text: "*Policy*\n" + label(in.PolicyName)})
	if in.Approval.ExpiresAt.Valid {
		fields = append(fields, slackdm.TextObject{Type: "mrkdwn", Text: "*Expires*\n" + slackdm.Date(in.Approval.ExpiresAt.Time)})
	}
	if note := in.Approval.DecisionNote.String; in.Approval.DecisionNote.Valid && note != "" {
		fields = append(fields, slackdm.TextObject{Type: "mrkdwn", Text: "*Note*\n" + slackdm.Fit(note, maxReasonChars)})
	}
	return slackdm.Block{Type: "section", BlockID: "", Text: nil, Fields: fields, Elements: nil}
}

func detailSections(in cardInput) []slackdm.Block {
	blocks := []slackdm.Block{}
	if reason := in.Approval.Reason.String; in.Approval.Reason.Valid && reason != "" {
		blocks = append(blocks, slackdm.MrkdwnSection("*Reason, from the agent*\n"+slackdm.Quote(reason, maxReasonChars)))
	}
	if args, ok := argumentsSection(in.Approval.ArgumentsPreview); ok {
		blocks = append(blocks, args)
	}
	return blocks
}

// argumentsSection renders the redacted arguments preview as one field per
// argument, sorted by name and bounded so the card stays inside Slack's
// limits. It reports false when there are no arguments to show.
func argumentsSection(raw []byte) (slackdm.Block, bool) {
	var none slackdm.Block
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
	hidden := takeOmitted(arguments)
	if len(arguments) == 0 && hidden == 0 {
		return none, false
	}
	names := slices.Sorted(maps.Keys(arguments))
	// The last field says how many arguments are not shown, when any are.
	if len(names) > maxArgumentFields || (len(names) == maxArgumentFields && hidden > 0) {
		hidden += len(names) - (maxArgumentFields - 1)
		names = names[:maxArgumentFields-1]
	}
	fields := make([]slackdm.TextObject, 0, len(names)+1)
	for _, name := range names {
		fields = append(fields, slackdm.TextObject{Type: "mrkdwn", Text: argumentName(name) + "\n" + argumentValue(arguments[name])})
	}
	if hidden > 0 {
		fields = append(fields, slackdm.TextObject{Type: "mrkdwn", Text: fmt.Sprintf("*More*\n%d more arguments, in Speakeasy", hidden)})
	}
	return slackdm.Block{Type: "section", BlockID: "", Text: &slackdm.TextObject{Type: "mrkdwn", Text: "*Arguments*"}, Fields: fields, Elements: nil}, true
}

// takeOmitted removes the preview's entry for arguments it left out and
// returns how many it left out. When an argument shares the mark, the
// preview's entry has the longest key.
func takeOmitted(arguments map[string]any) int {
	marker := ""
	hidden := 0
	for key, value := range arguments {
		note, isText := value.(string)
		if !isText || key == "" || strings.Trim(key, previewOmittedMark) != "" || len(key) <= len(marker) {
			continue
		}
		var count int
		if _, err := fmt.Sscanf(note, previewOmittedNote, &count); err != nil {
			continue
		}
		marker, hidden = key, count
	}
	if marker != "" {
		delete(arguments, marker)
	}
	return hidden
}

// argumentValue renders one argument. Plain text reads as text. Everything
// else reads as compact JSON in code: numbers, booleans and null, so the
// string "true" and the boolean true look different, and text that Slack
// would format, so an argument cannot pass for the card's own wording. The
// preview stores nested values as their JSON text, which reads as code as is.
func argumentValue(value any) string {
	text, isText := value.(string)
	if isText && nestedArgument(text) {
		return slackdm.Code(text, maxArgumentValueChars)
	}
	if isText && slackdm.ShowsAsWritten(text) {
		return slackdm.Fit(text, maxArgumentValueChars)
	}
	encoded, ok := compactJSON(value)
	if !ok {
		return "_cannot show this value_"
	}
	return slackdm.Code(encoded, maxArgumentValueChars)
}

// argumentName renders an argument's name as its field's bold title. A name
// Slack would format reads as code instead, so it cannot end the title early
// or add lines that pass for the card's own.
func argumentName(name string) string {
	if slackdm.ShowsAsWritten(name) {
		return "*" + slackdm.Fit(name, maxArgumentNameChars) + "*"
	}
	encoded, ok := compactJSON(name)
	if !ok {
		return "*Argument*"
	}
	return slackdm.Code(encoded, maxArgumentNameChars)
}

// label renders a person, server or policy name on one line: as text when
// Slack shows it as written, as code otherwise, so a name cannot format
// itself into the card's own wording.
func label(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" || slackdm.ShowsAsWritten(text) {
		return slackdm.Fit(text, maxNameChars)
	}
	return slackdm.Code(text, maxNameChars)
}

// compactJSON encodes value on one line, leaving <, > and & for slackdm.Fit to
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

// nestedArgument reports the JSON text the preview stores for a nested value.
func nestedArgument(text string) bool {
	trimmed := strings.TrimSpace(text)
	return (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) && !strings.ContainsAny(text, "\r\n")
}
