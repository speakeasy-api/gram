package mcpconnectionattempts

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
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

func TestCredited(t *testing.T) {
	t.Parallel()
	user := urn.NewUserSubject("user-credited").String()
	agent := urn.NewAgentSubject(uuid.New()).String()
	workload := urn.NewWorkloadSubject(uuid.New(), "external").String()
	cases := []struct {
		name       string
		users      uint64
		firstUser  string
		rowSubject string
		want       string
	}{
		{"row names a user", 1, user, user, user},
		{"row names a non-user beside a user", 1, user, agent, agent},
		{"row names no one, one user among other subjects", 1, user, "", user},
		{"row names no one, two users", 2, user, "", ""},
		{"row names no one, only non-users", 0, "", "", ""},
		{"row names a non-user, only non-users", 0, "", workload, workload},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			a := attemptFacts{Users: c.users, FirstUser: c.firstUser}
			require.Equal(t, c.want, a.credited(c.rowSubject))
		})
	}
}
