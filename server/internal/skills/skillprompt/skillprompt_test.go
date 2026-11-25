package skillprompt_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/skills/skillprompt"
	"github.com/speakeasy-api/gram/server/internal/skills/skilltree"
)

func file(path, content string) skilltree.File {
	return skilltree.File{Path: path, Content: []byte(content), ContentType: ""}
}

func TestSelect_OrdersReferencesFirstAndScriptsLast(t *testing.T) {
	t.Parallel()

	selection := skillprompt.Select([]skilltree.File{
		file("scripts/run.sh", "echo run\n"),
		file("assets/template.txt", "Dear team,\n"),
		file("references/z.md", "z\n"),
		file("references/a.md", "a\n"),
	}, 1024)

	paths := make([]string, len(selection.Included))
	for i, included := range selection.Included {
		paths[i] = included.Path
	}
	require.Equal(t, []string{"references/a.md", "references/z.md", "assets/template.txt", "scripts/run.sh"}, paths)
	require.Empty(t, selection.Omitted)
}

func TestSelect_ListsBinaryAndOverBudgetFilesByPath(t *testing.T) {
	t.Parallel()

	selection := skillprompt.Select([]skilltree.File{
		{Path: "assets/logo.png", Content: []byte{0x89, 'P', 'N', 'G', 0}, ContentType: "image/png"},
		file("references/big.md", strings.Repeat("x", 80)),
		file("references/small.md", strings.Repeat("y", 30)),
		file("scripts/run.sh", strings.Repeat("z", 30)),
	}, 64)

	require.Equal(t, []skillprompt.File{
		{Path: "references/small.md", Content: strings.Repeat("y", 30)},
		{Path: "scripts/run.sh", Content: strings.Repeat("z", 30)},
	}, selection.Included, "a file too large for the budget does not crowd out smaller ones")
	require.Equal(t, []skillprompt.OmittedFile{
		{Path: "references/big.md", Reason: skillprompt.OmitBudget},
		{Path: "assets/logo.png", Reason: skillprompt.OmitBinary},
	}, selection.Omitted)
}

func TestSelect_ChargesTheJSONEncodedSize(t *testing.T) {
	t.Parallel()

	// 20 raw bytes of markup encode to 120 ("<" becomes "<"), so the
	// markup file does not fit a 64-byte budget even though its raw size does.
	markup := strings.Repeat("<", 20)
	selection := skillprompt.Select([]skilltree.File{
		file("references/markup.md", markup),
		file("references/plain.md", strings.Repeat("p", 60)),
	}, 64)

	require.Equal(t, []skillprompt.File{
		{Path: "references/plain.md", Content: strings.Repeat("p", 60)},
	}, selection.Included)
	require.Equal(t, []skillprompt.OmittedFile{
		{Path: "references/markup.md", Reason: skillprompt.OmitBudget},
	}, selection.Omitted)

	// The included content, encoded the way the prompts encode it, stays
	// within the budget.
	encoded, err := json.Marshal(selection.Included[0].Content)
	require.NoError(t, err)
	require.LessOrEqual(t, len(encoded)-2, 64)
}

func TestSelect_NoFiles(t *testing.T) {
	t.Parallel()

	selection := skillprompt.Select(nil, 1024)
	require.Empty(t, selection.Included)
	require.Empty(t, selection.Omitted)
}
