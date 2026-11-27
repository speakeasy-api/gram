package risk_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk"
)

func TestCoworkPromptCallFingerprintMasksSendTime(t *testing.T) {
	t.Parallel()

	sentAt := func(minute string) string {
		return "<system-reminder>The user's timezone is Europe/London. Message sent at Thu 2026-11-12 13:" + minute + " local time.</system-reminder>\nSort Priya Okafor 731-52-8806"
	}
	require.Equal(t, risk.CoworkPromptCallFingerprint(sentAt("36")), risk.CoworkPromptCallFingerprint(sentAt("37")), "only the send minute differs")
	require.NotEqual(t, risk.CoworkPromptCallFingerprint(sentAt("36")), risk.CoworkPromptCallFingerprint(sentAt("36")+" and 284-19-6637"), "a different prompt is a different call")

	inReminder := func(id string) string {
		return "<system-reminder>Payroll " + id + "</system-reminder>\nSort Priya Okafor"
	}
	require.NotEqual(t, risk.CoworkPromptCallFingerprint(inReminder("731-52-8806")), risk.CoworkPromptCallFingerprint(inReminder("284-19-6637")), "other reminder text still counts")

	plain := "Sort Priya Okafor 731-52-8806"
	sum := sha256.Sum256([]byte(plain))
	require.Equal(t, hex.EncodeToString(sum[:]), risk.CoworkPromptCallFingerprint(plain), "a prompt without a send time hashes as typed")
}
