package xaareadiness

import (
	"encoding/csv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderCSV_QuotesEverythingAndNeutralizesFormulas(t *testing.T) {
	t.Parallel()

	rows := []exportRow{
		{Server: `=HYPERLINK("https://evil.example")`, Project: "prod", State: "needs_connection", ResourceIndicator: "https://mcp.example/", Audience: "", ClientID: "0oaclient", Scopes: "read write", DeepLink: "https://tenant-admin.okta.example/admin/ai-agents/x"},
		{Server: `Notion "prod", east`, Project: "+plus", State: "connected", ResourceIndicator: "-dash", Audience: "https://auth.example", ClientID: "@at", Scopes: "\ttab", DeepLink: "\rcr"},
	}
	out := string(renderCSV(rows))

	want := "\"Server\",\"Project\",\"State\",\"Resource indicator\",\"Audience\",\"Client ID at resource\",\"Scopes\",\"Add resource connection\"\r\n" +
		"\"'=HYPERLINK(\"\"https://evil.example\"\")\",\"prod\",\"needs_connection\",\"https://mcp.example/\",\"\",\"0oaclient\",\"read write\",\"https://tenant-admin.okta.example/admin/ai-agents/x\"\r\n" +
		"\"Notion \"\"prod\"\", east\",\"'+plus\",\"connected\",\"'-dash\",\"https://auth.example\",\"'@at\",\"'\ttab\",\"'\rcr\"\r\n"
	require.Equal(t, want, out)

	// A strict reader round-trips it.
	parsed, err := csv.NewReader(strings.NewReader(out)).ReadAll()
	require.NoError(t, err)
	require.Len(t, parsed, 3)
	require.Equal(t, exportHeader, parsed[0])
	require.Equal(t, `'=HYPERLINK("https://evil.example")`, parsed[1][0])
}

func TestRenderMarkdown_EscapesTableLinkAndHTMLSyntax(t *testing.T) {
	t.Parallel()

	rows := []exportRow{{
		Server:            "a | b [c](x)\nnext",
		Project:           "p|q",
		State:             "needs_agent",
		ResourceIndicator: "<https://evil.example>",
		Audience:          "https://auth.example|x",
		ClientID:          "<a href=x>id</a>",
		Scopes:            `read & a\|b`,
		DeepLink:          "https://t-admin.okta.com/admin/ai-agents/[x]",
	}}
	out := string(renderMarkdown(rows))
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	require.Len(t, lines, 3)
	require.Equal(t, "| "+strings.Join(exportHeader, " | ")+" |", lines[0])
	require.Equal(t, `| a \| b \[c\](x) next | p\|q | needs_agent | &lt;https://evil.example&gt; | https://auth.example\|x | &lt;a href=x&gt;id&lt;/a&gt; | read &amp; a\\\|b | https://t-admin.okta.com/admin/ai-agents/\[x\] |`, lines[2])
}
