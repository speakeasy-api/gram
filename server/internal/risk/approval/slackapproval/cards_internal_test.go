package slackapproval

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
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
