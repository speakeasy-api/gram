package conv_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

func TestMarkdownToHTML_Basic(t *testing.T) {
	t.Parallel()

	out, err := conv.MarkdownToHTML([]byte("# Hello"))
	require.NoError(t, err)
	require.Contains(t, string(out), "<h1>Hello</h1>")
}

func TestMarkdownToHTML_Empty(t *testing.T) {
	t.Parallel()

	out, err := conv.MarkdownToHTML([]byte(""))
	require.NoError(t, err)
	require.Equal(t, "", string(out))
}

func TestMarkdownToHTML_Paragraph(t *testing.T) {
	t.Parallel()

	out, err := conv.MarkdownToHTML([]byte("Hello **world**!"))
	require.NoError(t, err)
	require.Contains(t, string(out), "<strong>world</strong>")
}

func TestMarkdownToHTML_FencedCode(t *testing.T) {
	t.Parallel()

	md := "```go\nfmt.Println(\"hi\")\n```\n"
	out, err := conv.MarkdownToHTML([]byte(md))
	require.NoError(t, err)
	require.Contains(t, string(out), "<code")
	require.Contains(t, string(out), "fmt.Println")
}

func TestMarkdownToHTML_Tables(t *testing.T) {
	t.Parallel()

	md := "| a | b |\n| - | - |\n| 1 | 2 |\n"
	out, err := conv.MarkdownToHTML([]byte(md))
	require.NoError(t, err)
	require.Contains(t, string(out), "<table>")
	require.Contains(t, string(out), "<td")
}

func TestMarkdownToHTML_StripsScriptTag(t *testing.T) {
	t.Parallel()

	out, err := conv.MarkdownToHTML([]byte("<script>alert('xss')</script>"))
	require.NoError(t, err)
	require.NotContains(t, string(out), "<script")
	require.NotContains(t, string(out), "alert(")
}

func TestMarkdownToHTML_StripsOnclickAttribute(t *testing.T) {
	t.Parallel()

	out, err := conv.MarkdownToHTML([]byte(`<a href="https://example.com" onclick="alert(1)">link</a>`))
	require.NoError(t, err)
	require.NotContains(t, string(out), "onclick")
	require.NotContains(t, string(out), "alert(1)")
}

func TestMarkdownToHTML_StripsJavascriptHref(t *testing.T) {
	t.Parallel()

	out, err := conv.MarkdownToHTML([]byte(`[click](javascript:alert(1))`))
	require.NoError(t, err)
	require.NotContains(t, string(out), "javascript:")
}

func TestMarkdownToHTML_StripsImgOnerror(t *testing.T) {
	t.Parallel()

	out, err := conv.MarkdownToHTML([]byte(`<img src=x onerror="alert(1)">`))
	require.NoError(t, err)
	require.NotContains(t, string(out), "onerror")
	require.NotContains(t, string(out), "alert(1)")
}

func TestMarkdownToHTML_ExternalLinkAddsNofollow(t *testing.T) {
	t.Parallel()

	// bluemonday's UGCPolicy strips target/noopener/noreferrer that the markdown
	// renderer adds and applies its own rel="nofollow" to external links.
	out, err := conv.MarkdownToHTML([]byte(`[ext](https://example.com)`))
	require.NoError(t, err)
	rendered := string(out)
	require.Contains(t, rendered, `href="https://example.com"`)
	require.Contains(t, rendered, "nofollow")
}

func TestMarkdownToHTML_AutolinkRendered(t *testing.T) {
	t.Parallel()

	out, err := conv.MarkdownToHTML([]byte("Visit https://example.com here."))
	require.NoError(t, err)
	require.Contains(t, string(out), `href="https://example.com"`)
}
