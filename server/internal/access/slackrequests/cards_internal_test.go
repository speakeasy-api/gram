package slackrequests

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/slackdm"
)

func testInput(status string) cardInput {
	return cardInput{
		RequestID:       uuid.New(),
		Status:          status,
		RequesterName:   "Ada Member",
		PermissionLabel: "View chats",
		PermissionScope: "chat:read",
		ResourceName:    "",
		Message:         "Investigating a <report>",
		DeciderName:     "Grace Admin",
		ExpiresAt:       time.Now().Add(time.Hour),
		GrantExpiresAt:  time.Now().Add(2 * time.Hour),
		DashboardURL:    "https://app.speakeasy.test/org/access/roles?request=x",
	}
}

func actionIDs(t *testing.T, in cardInput, endOfDay time.Time) []string {
	t.Helper()
	ids := []string{}
	for _, element := range actionElements(t, in, endOfDay) {
		ids = append(ids, element.ActionID)
	}
	return ids
}

func actionElements(t *testing.T, in cardInput, endOfDay time.Time) []slackdm.Element {
	t.Helper()
	for _, block := range pendingCard(in, endOfDay).Blocks {
		if block.Type == "actions" {
			return block.Elements
		}
	}
	t.Fatal("pending card has no actions block")
	return nil
}

func menuChoices(t *testing.T, in cardInput, endOfDay time.Time) []string {
	t.Helper()
	labels := []string{}
	for _, option := range actionElements(t, in, endOfDay)[0].Options {
		labels = append(labels, option.Text.Text)
	}
	return labels
}

func TestPendingCardOffersATimeMenuAllowAndDeny(t *testing.T) {
	t.Parallel()
	in := testInput(access.AccessRequestStatusPending)

	require.Equal(t, []string{actionWindow, actionAllow, actionDeny, actionOpen}, actionIDs(t, in, time.Time{}))
	elements := actionElements(t, in, time.Time{})
	require.Equal(t, "static_select", elements[0].Type)
	require.Nil(t, elements[0].Confirm, "picking a time does not approve")
	require.Equal(t, "1 hour", elements[0].InitialOption.Text.Text, "Allow works without touching the menu")
	require.Equal(t, "primary", elements[1].Style, "Allow is the green button")
	require.Equal(t, []string{"1 hour", "2 hours", "4 hours"}, menuChoices(t, in, time.Time{}), "End of day is left out without a deadline")
	require.Equal(t, []string{"1 hour", "2 hours", "4 hours", "End of day"}, menuChoices(t, in, time.Now().Add(3*time.Hour)))

	in.DashboardURL = ""
	require.NotContains(t, actionIDs(t, in, time.Time{}), actionOpen)
}

func TestPendingCardEscapesTheRequesterMessage(t *testing.T) {
	t.Parallel()
	card := pendingCard(testInput(access.AccessRequestStatusPending), time.Time{})
	found := false
	for _, block := range card.Blocks {
		if block.Text != nil && block.Text.Text == "*Message, from the requester*\n> Investigating a &lt;report&gt;" {
			found = true
		}
	}
	require.True(t, found, "the message is quoted and escaped so it cannot pass for the card's own wording")
}

func TestEndOfDayIsHiddenWhenTheDayIsNearlyOver(t *testing.T) {
	t.Parallel()
	loc := time.FixedZone("test", -5*60*60)

	morning := time.Date(2026, 11, 17, 9, 0, 0, 0, loc)
	require.Equal(t, time.Date(2026, 11, 17, 23, 59, 0, 0, loc), endOfDay(morning, loc))

	late := time.Date(2026, 11, 17, 23, 52, 0, 0, loc)
	require.True(t, endOfDay(late, loc).IsZero())
}

func TestWindowValues(t *testing.T) {
	t.Parallel()
	deadline := time.Unix(1_800_000_000, 0)

	window, gotDeadline, ok := parseWindow("2h")
	require.True(t, ok)
	require.Equal(t, "2h", window)
	require.True(t, gotDeadline.IsZero())
	require.Equal(t, 2*time.Hour, windowDuration(window))

	window, gotDeadline, ok = parseWindow("eod|1800000000")
	require.True(t, ok)
	require.Equal(t, endOfDayKey, window)
	require.True(t, deadline.Equal(gotDeadline))

	for _, bad := range []string{"", "3h", "eod", "eod|soon", "1h|1800000000"} {
		_, _, ok = parseWindow(bad)
		require.False(t, ok, bad)
	}
}

func TestRequesterOutcome(t *testing.T) {
	t.Parallel()
	for _, status := range []string{access.AccessRequestStatusApproved, access.AccessRequestStatusDenied, access.AccessRequestStatusExpired} {
		_, ok := requesterOutcome(testInput(status))
		require.True(t, ok, status)
	}
	_, ok := requesterOutcome(testInput(access.AccessRequestStatusWithdrawn))
	require.False(t, ok, "a requester who withdrew needs no DM")
}
