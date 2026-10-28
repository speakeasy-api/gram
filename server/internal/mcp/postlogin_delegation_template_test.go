package mcp

import (
	"bytes"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

func TestPostLoginDelegationPreselectsExistingConnections(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	err := postLoginDelegationTemplate.Execute(&out, postLoginDelegationData{AccountName: "Example account", Options: []delegationOption{{ID: "renew", Name: "Renew agent", BindingID: "previous", Detail: "Your connection", Existing: true, Selected: true}, {ID: "other", Name: "Other connection", Disabled: true}}})
	require.NoError(t, err)
	html := out.String()
	require.Contains(t, html, "Example account")
	require.Contains(t, html, `value="renew" data-binding-id="previous" checked`)
	require.NotContains(t, html, "Add other agents")
	require.Contains(t, html, "data-grant-agents")
	require.Contains(t, html, ">Skip</button>")
	require.Contains(t, html, `type="submit" name="browser_redirect" value="1" data-skip-delegation`)
	require.Contains(t, html, `name="action" value="finish_delegation"`)
	require.Contains(t, html, "data-delegation-fallback")
	require.NotContains(t, html, `value="other"`)
}

func TestPostLoginDelegationEscapesAccountAndAgentNames(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	err := postLoginDelegationTemplate.Execute(&out, postLoginDelegationData{AccountName: "<script>account</script>", Options: []delegationOption{{Name: "<script>agent</script>", Existing: true}}})
	require.NoError(t, err)
	require.NotContains(t, out.String(), "<script>account")
	require.NotContains(t, out.String(), "<script>agent")
	require.Contains(t, out.String(), "&lt;script&gt;")
}

func TestPostLoginDelegationShowsIncreasedScopes(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	option := delegationOption{Name: "Example agent", Existing: true, ComparisonKnown: true, PreviousScopes: []string{"tools:read"}}
	describeDelegationPermissions(&option, []string{"tools:read", "tools:write"})
	err := postLoginDelegationTemplate.Execute(&out, postLoginDelegationData{Options: []delegationOption{option}})
	require.NoError(t, err)
	require.Contains(t, out.String(), "Previous permissions")
	require.Contains(t, out.String(), "Permissions after approval")
	require.Contains(t, out.String(), "tools:write")
	require.Equal(t, 1, strings.Count(out.String(), "· Added"))
	require.Contains(t, out.String(), "Grant 1 extra permission")
}

func TestDelegationExpiry(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name     string
		deadline *time.Time
		expected string
	}{
		{"expired", new(now.Add(-time.Hour)), "Expired"},
		{"soon", new(now.Add(90 * time.Minute)), "Expires in 1 hour"},
		{"unbounded", nil, "Never expires"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			label, _ := delegationExpiry(tc.deadline, now)
			require.Equal(t, tc.expected, label)
		})
	}
}
func TestDelegationUnknownHistoryDoesNotInventScopeDelta(t *testing.T) {
	t.Parallel()
	option := delegationOption{ComparisonKnown: false}
	describeDelegationPermissions(&option, []string{"tools:write"})
	require.Equal(t, "Review permissions", option.PermissionSummary)
	require.False(t, option.Scopes[0].Added)
}

func TestPostLoginDelegationOtherConnectionsStayUnselected(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	err := postLoginDelegationTemplate.Execute(&out, postLoginDelegationData{Options: []delegationOption{
		{ID: "other-owner", Name: "Workspace helper", Detail: "Jordan Example's connection", Disabled: true},
		{ID: "shared", Name: "Service helper", Detail: "Shared credential", Disabled: true},
	}})
	require.NoError(t, err)
	html := out.String()
	require.NotContains(t, html, "Jordan Example&#39;s connection")
	require.NotContains(t, html, "Shared credential")
	require.NotContains(t, html, `data-binding-id="" checked`)
	require.NotContains(t, html, "Workspace helper")
}

func TestConnectionScopeSummary(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		scopes []string
		want   string
	}{
		{nil, ""},
		{[]string{"read"}, "read"},
		{[]string{"read", "write"}, "read, write"},
		{[]string{"read", "write", "admin"}, "read, write, admin"},
		{[]string{"read", "write", "admin", "profile"}, "read, write & 2 others"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, (remoteSessionCard{Scopes: tc.scopes}).ScopeSummary())
		})
	}
}

func TestDelegationAgentSummary(t *testing.T) {
	t.Parallel()
	options := []delegationOption{{Name: "One", Selected: true}, {Name: "Two", Selected: true}, {Name: "Three", Selected: true}, {Name: "Four", Selected: true}, {Name: "Five", Selected: true}}
	for n, want := range []string{"", "One", "One and Two", "One, Two, and Three", "One, Two, and 2 others", "One, Two, and 3 others"} {
		t.Run(want, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, want, (postLoginDelegationData{Options: options[:n]}).AgentSummary())
		})
	}
}
