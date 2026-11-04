package chattitle_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat/chattitle"
)

const cronPrompt = "[cron:0f0e0d0c-0b0a-4908-8706-050403020100 weekly-skill-review] Review the skill collection and report changes."

func TestCronJobTitleNamesTheJob(t *testing.T) {
	t.Parallel()

	title, ok := chattitle.CronJobTitle(cronPrompt)
	require.True(t, ok)
	require.Equal(t, "Cron: weekly-skill-review", title)

	title, ok = chattitle.CronJobTitle("  " + cronPrompt)
	require.True(t, ok)
	require.Equal(t, "Cron: weekly-skill-review", title)
}

func TestCronJobTitleIgnoresOtherPrompts(t *testing.T) {
	t.Parallel()

	for _, content := range []string{
		"",
		"Review the skill collection",
		"[cron:not-a-uuid weekly-skill-review] Review",
		"Please run [cron:0f0e0d0c-0b0a-4908-8706-050403020100 nightly] later",
		"[cron:0f0e0d0c-0b0a-4908-8706-050403020100 ] Review",
	} {
		_, ok := chattitle.CronJobTitle(content)
		require.False(t, ok, content)
	}
}

func TestCronJobTitleBoundsLongJobNames(t *testing.T) {
	t.Parallel()

	content := "[cron:0f0e0d0c-0b0a-4908-8706-050403020100 " + strings.Repeat("j", 200) + "] go"
	title, ok := chattitle.CronJobTitle(content)
	require.True(t, ok)
	require.Len(t, []rune(title), chattitle.MaxRunes)
}

func TestDisplayRenamesStoredCronEnvelopes(t *testing.T) {
	t.Parallel()

	// A cron chat captured before cron titles existed stored the envelope, cut
	// to the derived-title length.
	require.Equal(t, "Cron: weekly-skill-review", chattitle.Display(cronPrompt[:chattitle.MaxRunes], false))
	require.Equal(t, "Review the skill collection", chattitle.Display("Review the skill collection", false))
}

func TestDisplayKeepsManualTitles(t *testing.T) {
	t.Parallel()

	require.Equal(t, cronPrompt, chattitle.Display(cronPrompt, true))
}
