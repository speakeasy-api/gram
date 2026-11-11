package authz

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var updateDocs = flag.Bool("update-docs", false, "rewrite the generated scope table in docs/rbac.md")

const (
	scopeTableBegin = "<!-- scope-table:begin -->"
	scopeTableEnd   = "<!-- scope-table:end -->"
)

// The scope table in docs/rbac.md is a rendering of the catalogue. This test
// fails when the two drift; `go test ./internal/authz -run DocTable -update-docs`
// rewrites the table from the catalogue.
func TestScopeCatalog_DocTableIsCurrent(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "..", "docs", "rbac.md")
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	doc := string(content)

	begin := strings.Index(doc, scopeTableBegin)
	end := strings.Index(doc, scopeTableEnd)
	require.True(t, begin >= 0 && end > begin, "docs/rbac.md lacks the %s / %s markers", scopeTableBegin, scopeTableEnd)
	begin += len(scopeTableBegin)

	// Blank lines on both sides are what the Markdown formatter keeps.
	want := "\n\n" + renderScopeTable(t, ScopeCatalog()) + "\n\n"
	if *updateDocs {
		require.NoError(t, os.WriteFile(path, []byte(doc[:begin]+want+doc[end:]), 0o600))
		return
	}
	require.Equal(t, want, doc[begin:end], "the scope table in docs/rbac.md is stale; run `go test ./internal/authz -run DocTable -update-docs` from server/")
}

// renderScopeTable writes the catalogue as a Markdown table in the layout the
// repository's formatter produces, so the committed docs pass its check. A
// catalogue entry without a visibility fails the test rather than being
// documented under a default.
func renderScopeTable(t *testing.T, entries []ScopeEntry) string {
	t.Helper()

	header := []string{"Scope", "Label", "Category", "Visibility", "Sensitive", "Includes", "Description"}
	rows := make([][]string, 0, len(entries))
	for _, entry := range entries {
		visibility, ok := entry.Scope.Visibility()
		require.True(t, ok, "%s: no visibility in the catalogue", entry.Scope)
		sensitive := "no"
		if entry.Sensitive() {
			sensitive = string(entry.Sensitivity)
		}
		includes := "—"
		if subs := entry.SubScopes(); len(subs) > 0 {
			quoted := make([]string, 0, len(subs))
			for _, sub := range subs {
				quoted = append(quoted, "`"+sub+"`")
			}
			includes = strings.Join(quoted, ", ")
		}
		rows = append(rows, []string{
			"`" + string(entry.Scope) + "`",
			entry.Label,
			string(entry.Category),
			visibility,
			sensitive,
			includes,
			strings.ReplaceAll(entry.Description, "|", "\\|"),
		})
	}

	widths := make([]int, len(header))
	for i, cell := range header {
		widths[i] = max(widths[i], displayWidth(cell))
	}
	for _, row := range rows {
		for i, cell := range row {
			widths[i] = max(widths[i], displayWidth(cell))
		}
	}

	var out strings.Builder
	writeRow := func(cells []string) {
		out.WriteString("|")
		for i, cell := range cells {
			fmt.Fprintf(&out, " %s%s |", cell, strings.Repeat(" ", widths[i]-displayWidth(cell)))
		}
		out.WriteString("\n")
	}
	writeRow(header)
	out.WriteString("|")
	for _, width := range widths {
		fmt.Fprintf(&out, " %s |", strings.Repeat("-", width))
	}
	out.WriteString("\n")
	for _, row := range rows {
		writeRow(row)
	}
	return strings.TrimRight(out.String(), "\n")
}

// displayWidth counts characters, not bytes, so an em dash pads like a letter.
func displayWidth(s string) int {
	return len([]rune(s))
}
