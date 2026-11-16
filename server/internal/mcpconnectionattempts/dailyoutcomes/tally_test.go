package dailyoutcomes

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
)

func TestTallyRowsFoldsNamesSharedByTooFewOrganizations(t *testing.T) {
	t.Parallel()

	const (
		google = "accounts.google.com"
		linear = "linear.app"
		claude = "claude.ai"
		code   = mcpconnectionattempts.FamilyClaudeCode
		dcr    = "dcr"
		other  = mcpconnectionattempts.LabelOther
	)
	type attempt struct{ org, provider, app, client string }
	key := func(provider, app, client string) string { return provider + "|" + app + "|" + client }

	tests := []struct {
		name     string
		attempts []attempt
		want     map[string]uint64
	}{
		{
			name:     "a client family alone is kept when three organizations share it",
			attempts: []attempt{{"a", "", "", code}, {"b", "", "", code}, {"c", "", "", code}},
			want:     map[string]uint64{key("", "", code): 3},
		},
		{
			name:     "a client family alone folds when two organizations share it",
			attempts: []attempt{{"a", "", "", code}, {"b", "", "", code}},
			want:     map[string]uint64{key("", "", other): 2},
		},
		{
			name:     "an app kind names nothing and never folds",
			attempts: []attempt{{"a", "", dcr, ""}},
			want:     map[string]uint64{key("", dcr, ""): 1},
		},
		{
			name:     "a rare provider folds but the app kind beside it stays",
			attempts: []attempt{{"a", linear, dcr, ""}},
			want:     map[string]uint64{key(other, dcr, ""): 1},
		},
		{
			name: "a rare app folds first, then its provider, keeping a shared client family",
			attempts: []attempt{
				{"a", google, claude, code},
				{"b", "", "", code}, {"c", "", "", code}, {"d", "", "", code},
			},
			want: map[string]uint64{key(other, other, code): 1, key("", "", code): 3},
		},
		{
			name: "a rare app folds alone when its provider and client family are shared",
			attempts: []attempt{
				{"a", google, claude, code},
				{"b", google, "", code}, {"c", google, "", code}, {"d", google, "", code},
			},
			want: map[string]uint64{key(google, other, code): 1, key(google, "", code): 3},
		},
		{
			name:     "a row named by one organization loses every name",
			attempts: []attempt{{"a", google, claude, code}},
			want:     map[string]uint64{key(other, other, other): 1},
		},
		{
			name: "the names are checked together, not one at a time",
			attempts: []attempt{
				{"a", google, claude, code}, {"b", google, claude, ""}, {"c", linear, claude, code},
			},
			want: map[string]uint64{key(other, other, other): 2, key(other, other, ""): 1},
		},
		{
			name: "Speakeasy's own organization does not count but its attempts are kept",
			attempts: []attempt{
				{"a", google, "", ""}, {"b", google, "", ""}, {constants.SpeakeasyTeamOrganizationID, google, "", ""},
			},
			want: map[string]uint64{key(other, "", ""): 3},
		},
		{
			name: "attempts from every organization add up on a kept row",
			attempts: []attempt{
				{"a", google, claude, code}, {"a", google, claude, code}, {"b", google, claude, code}, {"c", google, claude, code},
			},
			want: map[string]uint64{key(google, claude, code): 4},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			counts := newTally()
			ended := mcpconnectionattempts.Ending{Step: "inbound_token", Outcome: mcpconnectionattempts.OutcomeConnected, Reason: "", At: BackfillStart}
			for _, a := range tt.attempts {
				summary := mcpconnectionattempts.AttemptSummary{OrganizationID: a.org, ProviderHost: a.provider, AppKey: a.app, ClientFamily: a.client, Weight: 1}
				counts.add(summary, ended, mcpconnectionattempts.FixedByNothing)
			}
			got := map[string]uint64{}
			for _, row := range counts.rows() {
				got[key(row.Provider, row.App, row.ClientFamily)] += row.Attempts
			}
			require.Equal(t, tt.want, got)
			require.Empty(t, counts.unknown)
		})
	}
}
