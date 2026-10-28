package mcp

import (
	"bytes"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestPostLoginDelegationRequiresExplicitSelection(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	err := postLoginDelegationTemplate.Execute(&out, postLoginDelegationData{AccountName: "Example account", Options: []delegationOption{{ID: "renew", Name: "Renew agent", BindingID: "previous", Detail: "Renew previous connection"}, {ID: "other", Name: "Other connection", Disabled: true}}})
	require.NoError(t, err)
	html := out.String()
	require.Contains(t, html, "Example account")
	require.NotContains(t, html, "checked")
	require.Contains(t, html, "data-grant-agents")
	require.Contains(t, html, ">Skip</button>")
	require.Contains(t, html, `value="other" data-binding-id="" disabled`)
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
	err := postLoginDelegationTemplate.Execute(&out, postLoginDelegationData{
		HasPreviousScopes: true, PreviousScopes: []string{"tools:read"},
		Scopes: []delegationScope{{Name: "tools:read"}, {Name: "tools:write", Added: true}},
	})
	require.NoError(t, err)
	require.Contains(t, out.String(), "Previous connection")
	require.Contains(t, out.String(), "New connection")
	require.Contains(t, out.String(), "tools:write")
	require.Equal(t, 1, strings.Count(out.String(), "· Added"))
	require.NotContains(t, out.String(), "checked")
}
