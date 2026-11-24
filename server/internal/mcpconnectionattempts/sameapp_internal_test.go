package mcpconnectionattempts

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSameApp(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                      string
		failedApp, failedFamily   string
		successApp, successFamily string
		want                      bool
	}{
		{"unnamed failure, any app", AppDCR.key, "", AppOtherMetadataURL.key, FamilyOther, true},
		{"unnamed failure, a named app", AppDCR.key, FamilyOther, AppOtherMetadataURL.key, FamilyCursor, true},
		{"unnamed failure, the dashboard", AppDCR.key, "", AppFirstParty.key, FamilyBrowser, false},
		{"dashboard failure, the dashboard", AppFirstParty.key, FamilyBrowser, AppFirstParty.key, FamilyBrowser, true},
		{"named failure, same family under another key", AppDCR.key, FamilyCodexCLI, AppOtherMetadataURL.key, FamilyCodexCLI, true},
		{"named failure, another named family", AppDCR.key, FamilyCodexCLI, AppDCR.key, FamilyClaudeCode, false},
		{"named failure, unnamed under the same key", AppDCR.key, FamilyCodexCLI, AppDCR.key, FamilyOther, true},
		{"named failure under no verified app, unnamed under any key", AppUnverified.key, FamilyClaudeCode, AppDCR.key, FamilyOther, true},
		{"named failure, unnamed under another key", AppDCR.key, FamilyClaudeCode, AppOtherMetadataURL.key, FamilyOther, false},
	}
	for _, c := range cases {
		f := failure{a: attemptFacts{AttemptSummary: AttemptSummary{AppKey: c.failedApp, ClientFamily: c.failedFamily}}}
		require.Equal(t, c.want, f.sameApp(c.successApp, c.successFamily), c.name)
	}
}
