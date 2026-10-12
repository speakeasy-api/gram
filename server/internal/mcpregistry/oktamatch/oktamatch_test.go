package oktamatch

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKey(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"linear":                        "linear",
		"integrator-4080826_linear_1":   "linear",
		"integrator-4080826_supabase_1": "supabase",
		"realtime_board":                "realtimeboard",
		"github":                        "github",
		"Slack":                         "slack",
		"integrator-1_a-b_2":            "ab",
	} {
		require.Equal(t, want, Key(in), in)
	}
}

func TestRegistrableLabel(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"mcp.notion.com":        "notion",
		"mcp.linear.app":        "linear",
		"api.githubcopilot.com": "githubcopilot",
		"mcp.atlassian.co.uk":   "atlassian",
		"mcp-auth.granola.ai":   "granola",
		"localhost":             "",
		"app.datadoghq.com":     "datadoghq",
		"mcp.datadoghq.eu":      "datadoghq",
		"supabase.com":          "supabase",
		"www.canva.com":         "canva",
		"something.github.io":   "github",
		"mcp.example.test":      "example",
	} {
		require.Equal(t, want, registrableLabel(in), in)
	}
}

func TestMatch(t *testing.T) {
	t.Parallel()
	observed := []Observed{
		{Name: "notion", Labels: []string{"Notion", "Notion SAML (XAA probe)"}, SignOnModes: []string{"BROWSER_PLUGIN", "SAML_2_0"}, Organizations: 3},
		{Name: "integrator-4080826_linear_1", Labels: []string{"Linear"}, SignOnModes: []string{"SAML_2_0"}, Organizations: 1},
		{Name: "linear", Labels: []string{"Linear - XAA"}, SignOnModes: []string{"SAML_2_0"}, Organizations: 5},
		{Name: "realtime_board", Labels: []string{"Miro"}, SignOnModes: []string{"SAML_2_0"}, Organizations: 2},
		{Name: "github", Labels: []string{"Github Team"}, SignOnModes: []string{"AUTO_LOGIN"}, Organizations: 4},
		{Name: "okta_enduser", Labels: []string{"Okta Dashboard"}, SignOnModes: []string{"OPENID_CONNECT"}, Organizations: 9},
	}

	linear := EntryFromRecord(json.RawMessage(`{"server":{"name":"app.linear/mcp","title":"Linear","websiteUrl":"https://linear.app","remotes":[{"url":"https://mcp.linear.app/mcp"}]}}`))
	got := Match(linear, observed)
	require.Len(t, got, 2)
	require.Equal(t, "linear", got[0].Name)
	require.Equal(t, ReasonDomain, got[0].Reason)
	require.Equal(t, "integrator-4080826_linear_1", got[1].Name)
	require.Equal(t, ReasonDomain, got[1].Reason)

	// A vendor whose Okta key shares nothing with its domain matches on the
	// tenant label alone, ranked below domain matches.
	miro := EntryFromRecord(json.RawMessage(`{"server":{"name":"com.miro/mcp","title":"Miro","remotes":[{"url":"https://mcp.miro.com/mcp"}]}}`))
	got = Match(miro, observed)
	require.Len(t, got, 1)
	require.Equal(t, "realtime_board", got[0].Name)
	require.Equal(t, ReasonLabel, got[0].Reason)

	// GitHub's MCP host is githubcopilot.com; the entry name prefix carries
	// the vendor token instead.
	github := EntryFromRecord(json.RawMessage(`{"server":{"name":"com.github/mcp","title":"GitHub","remotes":[{"url":"https://api.githubcopilot.com/mcp/"}]}}`))
	got = Match(github, observed)
	require.Len(t, got, 1)
	require.Equal(t, "github", got[0].Name)
	require.Equal(t, ReasonDomain, got[0].Reason)

	// A title match ranks below a domain match and never comes from a
	// generic custom-app name, whatever the tenant labelled it.
	titled := EntryFromRecord(json.RawMessage(`{"server":{"name":"app.foo/mcp","title":"Notion","remotes":[{"url":"https://mcp.foo.com/mcp"}]}}`))
	got = Match(titled, append(observed, Observed{Name: "oidc_client", Labels: []string{"Notion"}, SignOnModes: []string{"OPENID_CONNECT"}, Organizations: 7}))
	require.Len(t, got, 1)
	require.Equal(t, "notion", got[0].Name)
	require.Equal(t, ReasonTitle, got[0].Reason)
	require.True(t, Integrator("integrator-4080826_linear_1"))
	require.False(t, Integrator("linear"))

	// Nothing matches an unrelated entry; Okta's own apps never do.
	require.Empty(t, Match(EntryFromRecord(json.RawMessage(`{"server":{"name":"io.example/fixture","title":"Fixture","remotes":[{"url":"https://mcp.example.test/mcp"}]}}`)), observed))
}
