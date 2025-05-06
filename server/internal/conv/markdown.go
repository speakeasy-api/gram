package conv

import (
	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
	"github.com/microcosm-cc/bluemonday"
)

var htmlPolicy = bluemonday.UGCPolicy()

func MarkdownToHTML(md []byte) ([]byte, error) {
	// create markdown parser with extensions
	extensions := parser.NoIntraEmphasis |
		parser.Tables |
		parser.FencedCode |
		parser.Autolink |
		parser.Strikethrough |
		parser.SpaceHeadings |
		parser.SuperSubscript |
		parser.EmptyLinesBreakList

	p := parser.NewWithExtensions(extensions)
	doc := p.Parse(md)

	// create HTML renderer with extensions
	renderer := html.NewRenderer(html.RendererOptions{
		Flags: html.CommonFlags |
			html.Safelink |
			html.HrefTargetBlank |
			html.NoopenerLinks |
			html.NoreferrerLinks |
			html.NofollowLinks,
	})

	rendered := markdown.Render(doc, renderer)
	sanitized := htmlPolicy.SanitizeBytes(rendered)

	return sanitized, nil
}
