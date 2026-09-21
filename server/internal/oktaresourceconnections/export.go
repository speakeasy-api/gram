package oktaresourceconnections

import (
	"strings"
)

// Export formats.
const (
	FormatCSV      = "csv"
	FormatMarkdown = "markdown"
)

var exportHeader = []string{"Server", "Project", "State", "Resource indicator", "Audience", "Client ID at resource", "Scopes", "Add resource connection"}

// exportRow is one checklist line; every value is admin-typed or Okta-sourced
// text and treated as untrusted.
type exportRow struct {
	Server            string
	Project           string
	State             string
	ResourceIndicator string
	Audience          string
	ClientID          string
	Scopes            string
	DeepLink          string
}

func (r exportRow) values() []string {
	return []string{r.Server, r.Project, r.State, r.ResourceIndicator, r.Audience, r.ClientID, r.Scopes, r.DeepLink}
}

// renderCSV writes RFC 4180 CSV with every field quoted (quotes doubled,
// CRLF line ends) and spreadsheet formula triggers neutralized: a cell that
// starts with = + - @ tab or CR gets a leading apostrophe so an Okta label
// cannot become a formula.
func renderCSV(rows []exportRow) []byte {
	var b strings.Builder
	writeCSVLine(&b, exportHeader, false)
	for _, r := range rows {
		writeCSVLine(&b, r.values(), true)
	}
	return []byte(b.String())
}

func writeCSVLine(b *strings.Builder, cells []string, neutralize bool) {
	for i, cell := range cells {
		if i > 0 {
			b.WriteByte(',')
		}
		if neutralize {
			cell = neutralizeFormula(cell)
		}
		b.WriteByte('"')
		b.WriteString(strings.ReplaceAll(cell, `"`, `""`))
		b.WriteByte('"')
	}
	b.WriteString("\r\n")
}

func neutralizeFormula(cell string) string {
	if cell == "" {
		return cell
	}
	switch cell[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + cell
	}
	return cell
}

// renderMarkdown writes a GitHub-flavoured table; pipes, brackets, HTML, and
// line breaks in cell text are escaped so a label cannot break the table,
// forge a link, or inject markup into a renderer that allows raw HTML.
func renderMarkdown(rows []exportRow) []byte {
	var b strings.Builder
	b.WriteString("| " + strings.Join(exportHeader, " | ") + " |\n")
	b.WriteString("|" + strings.Repeat(" --- |", len(exportHeader)) + "\n")
	for _, r := range rows {
		cells := r.values()
		for i := range cells {
			cells[i] = escapeMarkdownCell(cells[i])
		}
		b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}
	return []byte(b.String())
}

var markdownEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `\`, `\\`, "|", `\|`, "[", `\[`, "]", `\]`, "\n", " ", "\r", " ")

func escapeMarkdownCell(cell string) string {
	return markdownEscaper.Replace(cell)
}
