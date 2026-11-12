package risk_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk"
)

func TestPromptCallFingerprintSkipsLeadingReminders(t *testing.T) {
	t.Parallel()

	sentAt := func(minute string) string {
		return "<system-reminder>Message sent at Thu 2026-11-12 13:" + minute + " local time.</system-reminder>\nSort Priya Okafor 731-52-8806"
	}
	require.Equal(t, risk.PromptCallFingerprint(sentAt("36")), risk.PromptCallFingerprint(sentAt("37")), "only the reminder differs")
	require.Equal(t, risk.PromptCallFingerprint("Sort Priya Okafor 731-52-8806"), risk.PromptCallFingerprint(sentAt("36")), "the typed prompt is what counts")
	require.NotEqual(t, risk.PromptCallFingerprint(sentAt("36")), risk.PromptCallFingerprint(sentAt("36")+" and 284-19-6637"), "a different prompt is a different call")
	require.NotEmpty(t, risk.PromptCallFingerprint("<system-reminder>only framing</system-reminder>"), "a prompt that is only framing still gets a fingerprint")
}
