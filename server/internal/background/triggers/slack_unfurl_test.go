package triggers

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGramLinkTitleFromDashboardPaths(t *testing.T) {
	t.Parallel()

	cases := []struct {
		link  string
		title string
	}{
		{"https://app.getgram.ai/acme/projects/default/toolsets/my-tools", "My Tools · Toolsets"},
		{"https://app.getgram.ai/acme/projects/default/toolsets", "Toolsets"},
		{"https://app.getgram.ai/acme/projects/default/chat/6f1e0b6a-9f2c-4d3e-8a5b-0c1d2e3f4a5b", "Chat"},
		{"https://app.getgram.ai/acme/projects/default/assistants/support_bot/settings", "Settings · Assistants"},
		{"https://app.getgram.ai/acme/settings", "Settings"},
		{"https://app.getgram.ai/acme", "Gram dashboard"},
		{"https://app.getgram.ai/", "Gram dashboard"},
		{"https://app.getgram.ai/acme/projects/default/logs/12345", "Logs"},
	}

	for _, tc := range cases {
		parsed, err := url.Parse(tc.link)
		require.NoError(t, err)
		require.Equal(t, tc.title, gramLinkTitle(parsed), "link %s", tc.link)
	}
}

func TestHumanizeSlugTitleCasesWords(t *testing.T) {
	t.Parallel()

	require.Equal(t, "My Cool Toolset", humanizeSlug("my-cool-toolset"))
	require.Equal(t, "Support Bot", humanizeSlug("support_bot"))
	require.Equal(t, "Chat", humanizeSlug("chat"))
}

func TestEscapeSlackTextEscapesControlCharacters(t *testing.T) {
	t.Parallel()

	require.Equal(t, "A &amp; B &lt;C&gt;", escapeSlackText("A & B <C>"))
}
