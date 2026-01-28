package templatefuncs

import (
	"html/template"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

func FuncMap() template.FuncMap {
	return template.FuncMap{
		"diff":         Diff,
		"indent":       Indent,
		"asPosixName":  toolconfig.ToPosixName,
		"asHTTPHeader": toolconfig.ToHTTPHeader,
	}
}

func Diff(a, b int) int {
	return a - b
}

func Indent(spaces int, text string) string {
	if spaces <= 0 || text == "" {
		return text
	}
	indent := strings.Repeat(" ", spaces)
	lines := strings.Split(text, "\n")
	for i := 1; i < len(lines); i++ {
		if i == len(lines)-1 && lines[i] == "" {
			continue
		}
		lines[i] = indent + lines[i]
	}
	return strings.Join(lines, "\n")
}
