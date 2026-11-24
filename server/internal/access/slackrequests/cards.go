// Package slackrequests sends access requests to organization administrators
// as Slack DMs and takes their decisions back from Slack. Slack only
// notifies: every click goes through access.Service.DecideAccessRequestAs,
// the same path the dashboard uses.
package slackrequests

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/slackdm"
)

const (
	// ActionPrefix starts the action ID of every button on an access request
	// DM.
	ActionPrefix = "access_request."

	// actionWindow is the menu that picks how long an approval lasts.
	// Picking a window only changes the menu; Allow approves.
	actionWindow = ActionPrefix + "window"
	// actionAllow approves the request for the window the menu shows.
	actionAllow = ActionPrefix + "allow"
	actionDeny  = ActionPrefix + "deny"

	// actionOpen is the link button to the dashboard. Slack reports its
	// clicks too; the click handler ignores them.
	actionOpen = ActionPrefix + "open"

	maxNameChars    = 200
	maxMessageChars = 1500
)

// approveWindows are the fixed windows an approver can pick from Slack, in
// menu order. End of day is added per card, when the day has room for it.
var approveWindows = []struct {
	key      string
	label    string
	duration time.Duration
}{
	{key: "1h", label: "1 hour", duration: time.Hour},
	{key: "2h", label: "2 hours", duration: 2 * time.Hour},
	{key: "4h", label: "4 hours", duration: 4 * time.Hour},
}

// endOfDayKey picks End of day in the menu.
const endOfDayKey = "eod"

// cardInput is everything a card shows about one request.
type cardInput struct {
	RequestID       uuid.UUID
	Status          string
	RequesterName   string
	PermissionLabel string
	PermissionScope string
	ResourceName    string
	Message         string
	DeciderName     string
	ExpiresAt       time.Time
	GrantExpiresAt  time.Time
	DashboardURL    string
	// RequestedDuration is how long the requester asked for, counted from
	// approval, or zero.
	RequestedDuration time.Duration
	// RequestedUntil is when the requester asked to have the access until,
	// or zero.
	RequestedUntil time.Time
}

// pendingCard asks an administrator to decide. endOfDay is the deadline of
// the End of day choice, or zero to leave it out; cardEndOfDay picks it.
func pendingCard(in cardInput, endOfDay time.Time) slackdm.Message {
	requester := label(in.RequesterName)
	blocks := []slackdm.Block{
		slackdm.MrkdwnSection(fmt.Sprintf(":key: *Access request*: %s asks for %s", requester, label(in.PermissionLabel))),
		detailFields(in),
	}
	if in.Message != "" {
		blocks = append(blocks, slackdm.MrkdwnSection("*Message, from the requester*\n"+slackdm.Quote(in.Message, maxMessageChars)))
	}
	value := in.RequestID.String()
	options := make([]slackdm.Option, 0, len(approveWindows)+1)
	for _, window := range approveWindows {
		options = append(options, slackdm.Option{Text: slackdm.TextObject{Type: "plain_text", Text: window.label}, Value: window.key})
	}
	if !endOfDay.IsZero() {
		options = append(options, slackdm.Option{Text: slackdm.TextObject{Type: "plain_text", Text: "End of day"}, Value: endOfDayKey + "|" + strconv.FormatInt(endOfDay.Unix(), 10)})
	}
	initial := requestedWindowIndex(in.RequestedDuration)
	if !in.RequestedUntil.IsZero() && endOfDay.Equal(in.RequestedUntil) {
		// End of day carries the requester's own deadline; open on it. When
		// the card fell back to the reader's end of day, it is not what was
		// asked for, so the card opens on the fixed windows instead.
		initial = len(options) - 1
	}
	menu := slackdm.Element{
		Type:          "static_select",
		Text:          nil,
		TextValue:     "",
		ActionID:      actionWindow,
		Value:         "",
		URL:           "",
		Style:         "",
		Confirm:       nil,
		Placeholder:   slackdm.PlainText("Allow for…"),
		Options:       options,
		InitialOption: &options[initial],
	}
	allow := button("Allow", actionAllow, value, "primary")
	buttons := []slackdm.Element{menu, allow}
	deny := button("Deny", actionDeny, value, "danger")
	deny.Confirm = &slackdm.ConfirmDialog{
		Title: slackdm.TextObject{Type: "plain_text", Text: "Deny this request?"},
		// Slack shows a confirm dialog's text as plain text, so no mrkdwn here.
		Text:    slackdm.TextObject{Type: "plain_text", Text: "The requester is told their request was denied."},
		Confirm: slackdm.TextObject{Type: "plain_text", Text: "Deny"},
		Deny:    slackdm.TextObject{Type: "plain_text", Text: "Cancel"},
		Style:   "danger",
	}
	buttons = append(buttons, deny)
	if in.DashboardURL != "" {
		buttons = append(buttons, slackdm.Element{Type: "button", Text: slackdm.PlainText("Open in Speakeasy"), ActionID: actionOpen, Value: "", URL: in.DashboardURL, Style: "", TextValue: "", Confirm: nil, Placeholder: nil, Options: nil, InitialOption: nil})
	}
	blocks = append(blocks,
		slackdm.Block{Type: "actions", BlockID: "access_request", Text: nil, Fields: nil, Elements: buttons},
		slackdm.ContextBlock("Approving gives temporary access that ends on its own. To give it permanently, assign a role in Speakeasy."),
	)
	return slackdm.Message{Text: fmt.Sprintf("Access request: %s asks for %s", requester, label(in.PermissionLabel)), Blocks: blocks}
}

// decidedCard replaces a pending card once the request is decided, withdrawn
// or lapses. It keeps the details and drops the buttons.
func decidedCard(in cardInput) slackdm.Message {
	headline, text := decidedHeadline(in)
	blocks := []slackdm.Block{slackdm.MrkdwnSection(headline), detailFields(in)}
	if in.DashboardURL != "" {
		blocks = append(blocks, slackdm.ContextBlock(fmt.Sprintf("<%s|Open in Speakeasy>", in.DashboardURL)))
	}
	return slackdm.Message{Text: text, Blocks: blocks}
}

func decidedHeadline(in cardInput) (string, string) {
	requester := label(in.RequesterName)
	permission := label(in.PermissionLabel)
	decider := ""
	if in.DeciderName != "" {
		decider = " by " + label(in.DeciderName)
	}
	switch in.Status {
	case access.AccessRequestStatusApproved:
		until := ""
		if !in.GrantExpiresAt.IsZero() {
			until = " until " + slackdm.Date(in.GrantExpiresAt)
		}
		return fmt.Sprintf(":white_check_mark: *Approved*%s: %s has %s%s", decider, requester, permission, until), fmt.Sprintf("Approved: %s has %s", requester, permission)
	case access.AccessRequestStatusDenied:
		return fmt.Sprintf(":no_entry: *Denied*%s: %s asked for %s", decider, requester, permission), fmt.Sprintf("Denied: %s asked for %s", requester, permission)
	case access.AccessRequestStatusWithdrawn:
		return fmt.Sprintf(":leftwards_arrow_with_hook: *Withdrawn*: %s no longer needs %s", requester, permission), fmt.Sprintf("Withdrawn: %s no longer needs %s", requester, permission)
	case access.AccessRequestStatusExpired:
		return fmt.Sprintf(":hourglass: *Expired*: nobody decided on %s's request for %s in time", requester, permission), fmt.Sprintf("Expired: %s asked for %s", requester, permission)
	}
	return fmt.Sprintf(":key: *Waiting for a decision*: %s asks for %s", requester, permission), fmt.Sprintf("Waiting for a decision: %s asks for %s", requester, permission)
}

// requesterOutcome is the DM the requester gets once their request is
// approved, denied or lapses.
func requesterOutcome(in cardInput) (slackdm.Message, bool) {
	permission := label(in.PermissionLabel)
	var text string
	switch in.Status {
	case access.AccessRequestStatusApproved:
		text = fmt.Sprintf(":white_check_mark: Your request for %s was approved", permission)
		if in.DeciderName != "" {
			text += " by " + label(in.DeciderName)
		}
		if !in.GrantExpiresAt.IsZero() {
			text += ". Access ends " + slackdm.Date(in.GrantExpiresAt)
		}
		text += "."
	case access.AccessRequestStatusDenied:
		text = fmt.Sprintf(":no_entry: Your request for %s was denied", permission)
		if in.DeciderName != "" {
			text += " by " + label(in.DeciderName)
		}
		text += "."
	case access.AccessRequestStatusExpired:
		text = fmt.Sprintf(":hourglass: Your request for %s expired before an administrator decided it. You can ask again from Speakeasy.", permission)
	default:
		return slackdm.Message{Text: "", Blocks: nil}, false
	}
	blocks := []slackdm.Block{slackdm.MrkdwnSection(text)}
	if in.DashboardURL != "" {
		blocks = append(blocks, slackdm.ContextBlock(fmt.Sprintf("<%s|Open in Speakeasy>", in.DashboardURL)))
	}
	return slackdm.Message{Text: text, Blocks: blocks}, true
}

func detailFields(in cardInput) slackdm.Block {
	resource := in.ResourceName
	if resource == "" {
		resource = "Every resource"
	}
	fields := []slackdm.TextObject{
		{Type: "mrkdwn", Text: "*Requester*\n" + label(in.RequesterName)},
		{Type: "mrkdwn", Text: "*Permission*\n" + label(in.PermissionLabel) + " " + slackdm.Code(in.PermissionScope, maxNameChars)},
		{Type: "mrkdwn", Text: "*Resource*\n" + label(resource)},
	}
	if in.Status == access.AccessRequestStatusPending && in.RequestedDuration > 0 {
		fields = append(fields, slackdm.TextObject{Type: "mrkdwn", Text: "*Asked for*\n" + durationLabel(in.RequestedDuration)})
	}
	if in.Status == access.AccessRequestStatusPending && !in.RequestedUntil.IsZero() {
		// Slack renders the date in the reader's own time zone.
		fields = append(fields, slackdm.TextObject{Type: "mrkdwn", Text: "*Asked until*\n" + slackdm.Date(in.RequestedUntil)})
	}
	if in.Status == access.AccessRequestStatusPending && !in.ExpiresAt.IsZero() {
		fields = append(fields, slackdm.TextObject{Type: "mrkdwn", Text: "*Request expires*\n" + slackdm.Date(in.ExpiresAt)})
	}
	return slackdm.Block{Type: "section", BlockID: "", Text: nil, Fields: fields, Elements: nil}
}

func button(text, actionID, value, style string) slackdm.Element {
	return slackdm.Element{Type: "button", Text: slackdm.PlainText(text), ActionID: actionID, Value: value, URL: "", Style: style, TextValue: "", Confirm: nil, Placeholder: nil, Options: nil, InitialOption: nil}
}

// label renders a name on one line: as text when Slack shows it as written,
// as code otherwise, so a name cannot format itself into the card's wording.
func label(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" || slackdm.ShowsAsWritten(text) {
		return slackdm.Fit(text, maxNameChars)
	}
	return slackdm.Code(text, maxNameChars)
}

// requestedWindowIndex picks the fixed window a card opens on for the
// duration the requester asked for: the one that matches it, or else the
// longest one within it. With no duration, or one shorter than every window,
// it is the first.
func requestedWindowIndex(requested time.Duration) int {
	index := 0
	for i, window := range approveWindows {
		if window.duration <= requested {
			index = i
		}
	}
	return index
}

// cardEndOfDay is the deadline of a card's End of day choice: the time the
// requester asked for when they asked for one that is still far enough off,
// otherwise 23:59 today in loc, the zone the card is read in. It is zero when
// neither leaves enough time.
func cardEndOfDay(in cardInput, now time.Time, loc *time.Location) time.Time {
	if lead := in.RequestedUntil.Sub(now); !in.RequestedUntil.IsZero() && lead >= access.MinEndOfDayLead && lead <= access.TemporaryGrantMaxDuration {
		return in.RequestedUntil
	}
	return access.EndOfDay(now, loc)
}

// durationLabel reads a requested duration: a fixed window's label when it
// matches one, otherwise hours and minutes.
func durationLabel(duration time.Duration) string {
	for _, window := range approveWindows {
		if window.duration == duration {
			return window.label
		}
	}
	hours := int(duration / time.Hour)
	minutes := int((duration % time.Hour) / time.Minute)
	parts := make([]string, 0, 2)
	if hours > 0 {
		parts = append(parts, plural(hours, "hour"))
	}
	if minutes > 0 || hours == 0 {
		parts = append(parts, plural(minutes, "minute"))
	}
	return strings.Join(parts, " ")
}

func plural(count int, unit string) string {
	if count == 1 {
		return "1 " + unit
	}
	return strconv.Itoa(count) + " " + unit + "s"
}

// parseWindow reads a menu choice: a fixed window's key, or End of day with
// the deadline the card was sent with.
func parseWindow(value string) (window string, deadline time.Time, ok bool) {
	key, rawDeadline, hasDeadline := strings.Cut(value, "|")
	if key == endOfDayKey && hasDeadline {
		seconds, err := strconv.ParseInt(rawDeadline, 10, 64)
		if err != nil {
			return "", time.Time{}, false
		}
		return endOfDayKey, time.Unix(seconds, 0), true
	}
	if hasDeadline || windowDuration(key) == 0 {
		return "", time.Time{}, false
	}
	return key, time.Time{}, true
}

// windowDuration is the length of a fixed window, or 0 for an unknown key.
func windowDuration(key string) time.Duration {
	for _, window := range approveWindows {
		if window.key == key {
			return window.duration
		}
	}
	return 0
}
