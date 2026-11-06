package slackapp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHomeMCPCardUsesBrandingAndAction(t *testing.T) {
	t.Parallel()
	data := HomeData{Linked: true, Servers: []HomeServer{{Name: "Linear", ProjectName: "Engineering", URL: "https://example.com/linear", LogoURL: "https://api.example.com/logo.png"}}}
	encoded := encodeHome(t, data)
	require.Contains(t, encoded, `"type":"image"`)
	require.Contains(t, encoded, `"image_url":"https://api.example.com/logo.png"`)
	require.Contains(t, encoded, `"alt_text":"Linear logo"`)
	require.Contains(t, encoded, "*Linear*")
	require.Contains(t, encoded, "Open MCP")
	require.NotContains(t, encoded, "Open a server for details")
	require.NotContains(t, encoded, "Mention Speakeasy")
	data.Servers[0].LogoURL = ""
	require.NotContains(t, encodeHome(t, data), `"type":"image"`)
	require.Contains(t, encodeHome(t, data), "*Linear*")
}

func TestHomeCardLabelCannotInjectSlackMarkup(t *testing.T) {
	t.Parallel()
	require.Equal(t, "*Linear*", homeLabel("Linear", true))
	require.Equal(t, "Engineering", homeLabel("Engineering", false))
	require.Equal(t, "2 Jan 2026, 15:04 UTC", homeLabel("2 Jan 2026, 15:04 UTC", false))
	for _, text := range []string{"<@U123>", "<!channel>", "<https://evil.example|Review>", "*Forged title*", "_italic_", "`code`", ":wave:", "Title\n<!everyone>"} {
		label := homeLabel(text, true)
		require.NotContains(t, label, "<")
		require.NotContains(t, label, ">")
		require.NotContains(t, label, "\n")
		if strings.ContainsAny(text, "*_~`") || homeEmojiCode.MatchString(text) {
			require.True(t, strings.HasPrefix(label, "`") && strings.HasSuffix(label, "`"))
			require.Equal(t, 2, strings.Count(label, "`"))
		}
	}
	require.LessOrEqual(t, len([]rune(homeLabel(strings.Repeat("<", 4000), true))), 1100)
}

func TestHomeCardSkipsUnfetchableImages(t *testing.T) {
	t.Parallel()
	for _, image := range []string{"", "data:image/png;base64,example", "http://example.com/icon.png", "https://localhost/icon.png", "https://api.local/icon.png", "https://api.internal/icon.png", "https://127.0.0.1/icon.png", "https://10.0.0.1/icon.png", "https://[::1]/icon.png", "https://user:password@example.com/icon.png"} {
		t.Run(image, func(t *testing.T) {
			t.Parallel()
			require.False(t, homeImageURL(image))
			encoded := encodeHome(t, HomeData{Linked: true, Servers: []HomeServer{{Name: "Example", LogoURL: image}}})
			require.NotContains(t, encoded, `"type":"image"`)
		})
	}
	require.True(t, homeImageURL("https://api.example.com/rpc/assets.serveImage?id=logo"))
}
