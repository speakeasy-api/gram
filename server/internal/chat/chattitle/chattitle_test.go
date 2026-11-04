package chattitle_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat/chattitle"
)

const cronPrompt = "[cron:4a39cac2-54ab-4ef2-8c81-147832084a86 skill-collection-review-main] Review the skill collection and report changes."

func TestCronJobTitleNamesTheJob(t *testing.T) {
	t.Parallel()

	title, ok := chattitle.CronJobTitle(cronPrompt)
	require.True(t, ok)
	require.Equal(t, "Cron: skill-collection-review-main", title)

	title, ok = chattitle.CronJobTitle("  " + cronPrompt)
	require.True(t, ok)
	require.Equal(t, "Cron: skill-collection-review-main", title)
}

func TestCronJobTitleIgnoresOtherPrompts(t *testing.T) {
	t.Parallel()

	for _, content := range []string{
		"",
		"Review the skill collection",
		"[cron:not-a-uuid skill-collection-review-main] Review",
		"Please run [cron:4a39cac2-54ab-4ef2-8c81-147832084a86 nightly] later",
		"[cron:4a39cac2-54ab-4ef2-8c81-147832084a86 ] Review",
	} {
		_, ok := chattitle.CronJobTitle(content)
		require.False(t, ok, content)
	}
}

func TestCronJobTitleBoundsLongJobNames(t *testing.T) {
	t.Parallel()

	content := "[cron:4a39cac2-54ab-4ef2-8c81-147832084a86 " + strings.Repeat("j", 200) + "] go"
	title, ok := chattitle.CronJobTitle(content)
	require.True(t, ok)
	require.Len(t, []rune(title), chattitle.MaxRunes)
}

func TestDisplayRenamesStoredCronEnvelopes(t *testing.T) {
	t.Parallel()

	// A cron chat captured before cron titles existed stored the envelope, cut
	// to the derived-title length.
	require.Equal(t, "Cron: skill-collection-review-main", chattitle.Display(cronPrompt[:chattitle.MaxRunes], false))
	require.Equal(t, "Review the skill collection", chattitle.Display("Review the skill collection", false))
}

func TestDisplayKeepsManualTitles(t *testing.T) {
	t.Parallel()

	require.Equal(t, cronPrompt, chattitle.Display(cronPrompt, true))
}
