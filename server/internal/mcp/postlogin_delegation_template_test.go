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
	require.Contains(t, html, "Add other agents")
	require.Contains(t, html, "data-grant-agents")
	require.Contains(t, html, ">Skip</button>")
	require.Contains(t, html, `value="other" data-binding-id=""  disabled`)
}

func TestPostLoginDelegationEscapesAccountAndAgentNames(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	err := postLoginDelegationTemplate.Execute(&out, postLoginDelegationData{AccountName: "<script>account</script>", Options: []delegationOption{{Name: "<script>agent</script>"}}})
	require.NoError(t, err)
	require.NotContains(t, out.String(), "<script>account")
	require.NotContains(t, out.String(), "<script>agent")
	require.Contains(t, out.String(), "&lt;script&gt;")
}

func TestPostLoginDelegationShowsIncreasedScopes(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	option := delegationOption{Name: "Example agent", ComparisonKnown: true, PreviousScopes: []string{"tools:read"}}
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
	require.Contains(t, html, "Jordan Example&#39;s connection")
	require.Contains(t, html, "Shared credential")
	require.NotContains(t, html, ` checked`)
	require.Less(t, strings.Index(html, "Add other agents"), strings.Index(html, "Workspace helper"))
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
