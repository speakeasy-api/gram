package slackapproval

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk/approval"
	"github.com/speakeasy-api/gram/server/internal/risk/approval/repo"
)

func TestArgumentsSectionShowsOneFieldPerArgument(t *testing.T) {
	t.Parallel()

	section, ok := argumentsSection([]byte(`{"text":"Ship it","count":3,"dry_run":true,"owner":null,"labels":["a","b"],"note":"","quoted":"true"}`))
	require.True(t, ok)
	require.Equal(t, "*Arguments*", section.Text.Text)
	fields := make([]string, 0, len(section.Fields))
	for _, field := range section.Fields {
		fields = append(fields, field.Text)
	}
	// Sorted by name. Text reads as text; anything else reads as code, so the
	// string "true" and the boolean true differ.
	require.Equal(t, []string{
		"*count*\n`3`",
		"*dry_run*\n`true`",
		"*labels*\n`[\"a\",\"b\"]`",
		"*note*\n`\"\"`",
		"*owner*\n`null`",
		"*quoted*\ntrue",
		"*text*\nShip it",
	}, fields)
}

func TestArgumentsSectionStaysInsideSlackLimits(t *testing.T) {
	t.Parallel()

	var pairs []string
	for i := range 12 {
		pairs = append(pairs, fmt.Sprintf(`"arg%02d":"%s"`, i, strings.Repeat("&", 1000)))
	}
	section, ok := argumentsSection([]byte("{" + strings.Join(pairs, ",") + "}"))
	require.True(t, ok)
	require.Len(t, section.Fields, maxArgumentFields)
	require.Equal(t, "*More*\n3 more arguments, in Speakeasy", section.Fields[maxArgumentFields-1].Text)
	for _, field := range section.Fields {
		require.LessOrEqual(t, len(field.Text), 2000, "Slack caps a field at 2,000 characters")
	}

	_, ok = argumentsSection([]byte(`{}`))
	require.False(t, ok, "a call without arguments shows no arguments section")
	_, ok = argumentsSection(nil)
	require.False(t, ok)
}

func TestArgumentsSectionShowsTextSlackWouldFormatAsCode(t *testing.T) {
	t.Parallel()

	section, ok := argumentsSection([]byte(`{"bold":"*Approved* by security","check":":white_check_mark: Approved","emoji":"all good :tada:","lines":"one\ntwo","nested":"{\"a\":1}","snake":"dry_run","tick":"a` + "`" + `b"}`))
	require.True(t, ok)
	fields := make([]string, 0, len(section.Fields))
	for _, field := range section.Fields {
		fields = append(fields, field.Text)
	}
	// Formatting, emoji and line breaks stay literal, and the JSON text the
	// preview stores for a nested value reads as code.
	require.Equal(t, []string{
		"*bold*\n`\"*Approved* by security\"`",
		"*check*\n`\":white_check_mark: Approved\"`",
		"*emoji*\n`\"all good :tada:\"`",
		"*lines*\n`\"one\\ntwo\"`",
		"*nested*\n`{\"a\":1}`",
		"*snake*\ndry_run",
		"*tick*\n`\"a'b\"`",
	}, fields)
}

func TestCardTextStaysInsideSlackLimitsAfterEscaping(t *testing.T) {
	t.Parallel()

	section, ok := argumentsSection([]byte(fmt.Sprintf(`{"%s":"%s"}`, strings.Repeat("&", 500), strings.Repeat("<", 1000))))
	require.True(t, ok)
	require.Len(t, section.Fields, 1)
	field := section.Fields[0].Text
	require.LessOrEqual(t, utf8.RuneCountInString(field), maxArgumentNameChars+maxArgumentValueChars+10)
	require.True(t, strings.HasPrefix(field, "*&amp;"), "escaping is never cut in the middle: %q", field[:10])

	long := pgtype.Text{String: strings.Repeat("<\n", 1500), Valid: true}
	in := cardInput{
		Approval:      repo.RiskPolicyApproval{ToolName: "send_message", Reason: long, DecisionNote: pgtype.Text{String: strings.Repeat("&", 1500), Valid: true}},
		Status:        approval.StatusDenied,
		PolicyName:    "Policy",
		RequesterName: "Maya",
	}
	for _, block := range decidedCard(in).Blocks {
		if block.Text != nil {
			require.LessOrEqual(t, utf8.RuneCountInString(block.Text.Text), 3000, "Slack caps section text at 3,000 characters")
		}
		for _, field := range block.Fields {
			require.LessOrEqual(t, utf8.RuneCountInString(field.Text), 2000, "Slack caps a field at 2,000 characters")
		}
	}
}

func TestHeadlineKeepsTheToolNameInItsCodeSpan(t *testing.T) {
	t.Parallel()

	in := cardInput{
		Approval:      repo.RiskPolicyApproval{ToolName: "x` :white_check_mark: *Approved* `y"},
		Status:        approval.StatusPending,
		PolicyName:    "Policy",
		RequesterName: "Maya",
	}
	require.Equal(t, ":raised_hand: *Approval needed* to call `x' :white_check_mark: *Approved* 'y`", pendingCard(in, true).Blocks[0].Text.Text)
	headline, _ := decidedHeadline(in)
	require.Equal(t, ":raised_hand: *Waiting for a decision* on the call to `x' :white_check_mark: *Approved* 'y`", headline)

	// Slack reads the notification text as mrkdwn too.
	in.Approval.ToolName = "<!here> *Denied* :no_entry:"
	require.Equal(t, "Approval needed to call `&lt;!here&gt; *Denied* :no_entry:`", pendingCard(in, true).Text)
	_, text := decidedHeadline(in)
	require.Equal(t, "Waiting for a decision on the call to `&lt;!here&gt; *Denied* :no_entry:`", text)
}

func TestArgumentNamesCannotAddCardLines(t *testing.T) {
	t.Parallel()

	section, ok := argumentsSection([]byte(`{"_foo_":1,"dry_run":true,"x*\n*Requester*\nCEO":"y"}`))
	require.True(t, ok)
	require.Equal(t, "`\"_foo_\"`\n`1`", section.Fields[0].Text)
	require.Equal(t, "*dry_run*\n`true`", section.Fields[1].Text)
	require.Equal(t, "`\"x*\\n*Requester*\\nCEO\"`\ny", section.Fields[2].Text)
}

func TestArgumentsSectionCountsArgumentsThePreviewLeftOut(t *testing.T) {
	t.Parallel()

	section, ok := argumentsSection([]byte(`{"a":1,"b":2,"…":"arguments not shown: 5"}`))
	require.True(t, ok)
	require.Len(t, section.Fields, 3)
	require.Equal(t, "*More*\n5 more arguments, in Speakeasy", section.Fields[2].Text)

	// An argument named like the mark stays an argument; the preview's entry
	// has the longer key.
	section, ok = argumentsSection([]byte(`{"…":"x","……":"arguments not shown: 2"}`))
	require.True(t, ok)
	require.Equal(t, []string{"*…*\nx", "*More*\n2 more arguments, in Speakeasy"}, []string{section.Fields[0].Text, section.Fields[1].Text})

	var pairs []string
	for i := range 12 {
		pairs = append(pairs, fmt.Sprintf(`"arg%02d":%d`, i, i))
	}
	section, ok = argumentsSection([]byte("{" + strings.Join(pairs, ",") + `,"…":"arguments not shown: 3"}`))
	require.True(t, ok)
	require.Len(t, section.Fields, maxArgumentFields)
	require.Equal(t, "*More*\n6 more arguments, in Speakeasy", section.Fields[maxArgumentFields-1].Text)
}

func TestNamesSlackWouldFormatReadAsCode(t *testing.T) {
	t.Parallel()

	in := cardInput{
		Approval:      repo.RiskPolicyApproval{ToolName: "send_message"},
		Status:        approval.StatusPending,
		PolicyName:    "Policy",
		RequesterName: ":white_check_mark: *Pre-approved*\nby security",
	}
	require.Equal(t, "*Requester*\n`:white_check_mark: *Pre-approved* by security`", detailFields(in).Fields[0].Text)
	require.Equal(t, "*Policy*\nPolicy", detailFields(in).Fields[1].Text)
}
