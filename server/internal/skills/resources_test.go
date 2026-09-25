package skills

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSkillResourceReferencesSpecDirectories(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		"See [the reference guide](references/REFERENCE.md) for details.",
		"",
		"Run the extraction script:",
		"",
		"```bash",
		"python scripts/extract.py --out result.json",
		"```",
		"",
		"![architecture](assets/diagram.png)",
		"",
		"Fill in `assets/templates/report.docx` before sending.",
	}, "\n")

	require.Equal(t, []skillResourceReference{
		{Path: "assets/diagram.png", Kind: skillResourceKindAsset},
		{Path: "assets/templates/report.docx", Kind: skillResourceKindAsset},
		{Path: "references/REFERENCE.md", Kind: skillResourceKindReference},
		{Path: "scripts/extract.py", Kind: skillResourceKindScript},
	}, parseSkillResourceReferences(body))
}

func TestParseSkillResourceReferencesIgnoresNonFiles(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		"Read the [spec](https://agentskills.io/spec) and the [FAQ](http://example.com/faq.md).",
		"Jump to [the summary](#summary).",
		"Mail [us](mailto:support@example.com).",
		"An [absolute path](/etc/passwd) is not part of the skill.",
		"A [home path](~/notes.md) is not part of the skill.",
		"A [bare route](../overview) has no file extension.",
		"Inline data: ![pixel](data:image/png;base64,iVBORw0KGgo=)",
	}, "\n")

	require.Empty(t, parseSkillResourceReferences(body))
}

func TestParseSkillResourceReferencesRelativeSiblings(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		"Start with [the checklist](./CHECKLIST.md).",
		"Older notes live in [the archive](../shared/NOTES.md).",
		"Nested: [deep](docs/guides/advanced.md)",
	}, "\n")

	require.Equal(t, []skillResourceReference{
		{Path: "../shared/NOTES.md", Kind: skillResourceKindOther},
		{Path: "CHECKLIST.md", Kind: skillResourceKindOther},
		{Path: "docs/guides/advanced.md", Kind: skillResourceKindOther},
	}, parseSkillResourceReferences(body))
}

func TestParseSkillResourceReferencesNormalizesDestinations(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		"[anchor](references/REFERENCE.md#usage)",
		"[query](references/REFERENCE.md?v=2)",
		"[escaped](assets/report%20template.docx)",
		"[angle](<references/spaced name.md>)",
		"[titled](scripts/run.sh \"How to run\")",
		"[dot slash](./scripts/run.sh)",
		"[redundant](references/./nested/../REFERENCE.md)",
	}, "\n")

	require.Equal(t, []skillResourceReference{
		{Path: "assets/report template.docx", Kind: skillResourceKindAsset},
		{Path: "references/REFERENCE.md", Kind: skillResourceKindReference},
		{Path: "references/spaced name.md", Kind: skillResourceKindReference},
		{Path: "scripts/run.sh", Kind: skillResourceKindScript},
	}, parseSkillResourceReferences(body))
}

func TestParseSkillResourceReferencesLinkDefinitions(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		"Follow the [guide] and the [runner].",
		"",
		"[guide]: references/GUIDE.md",
		"   [runner]: <scripts/run.sh> \"Runner\"",
		"[external]: https://example.com/page.md",
	}, "\n")

	require.Equal(t, []skillResourceReference{
		{Path: "references/GUIDE.md", Kind: skillResourceKindReference},
		{Path: "scripts/run.sh", Kind: skillResourceKindScript},
	}, parseSkillResourceReferences(body))
}

func TestParseSkillResourceReferencesTrimsTrailingPunctuation(t *testing.T) {
	t.Parallel()

	body := "Run scripts/setup.sh, then read references/NEXT.md. Templates live in assets/base.json."

	require.Equal(t, []skillResourceReference{
		{Path: "assets/base.json", Kind: skillResourceKindAsset},
		{Path: "references/NEXT.md", Kind: skillResourceKindReference},
		{Path: "scripts/setup.sh", Kind: skillResourceKindScript},
	}, parseSkillResourceReferences(body))
}

func TestParseSkillResourceReferencesDeduplicates(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		"[once](scripts/run.sh) and [again](./scripts/run.sh).",
		"Then `scripts/run.sh` one more time.",
	}, "\n")

	require.Equal(t, []skillResourceReference{
		{Path: "scripts/run.sh", Kind: skillResourceKindScript},
	}, parseSkillResourceReferences(body))
}

func TestParseSkillResourceReferencesIgnoresBareDirectoryNames(t *testing.T) {
	t.Parallel()

	body := "Put helpers in scripts/ and docs in references/ as the spec suggests."

	require.Empty(t, parseSkillResourceReferences(body))
}

func TestParseSkillResourceReferencesBounded(t *testing.T) {
	t.Parallel()

	lines := make([]string, 0, maxSkillResourceReferences*2)
	for i := range maxSkillResourceReferences * 2 {
		lines = append(lines, "- scripts/step-"+string(rune('a'+i%26))+string(rune('a'+i/26))+".sh")
	}

	references := parseSkillResourceReferences(strings.Join(lines, "\n"))
	require.Len(t, references, maxSkillResourceReferences)
}

func TestParseSkillManifestReportsResourceReferences(t *testing.T) {
	t.Parallel()

	content := strings.Join([]string{
		"---",
		"name: pdf-processing",
		"description: Extracts text from PDFs. Use when handling PDF documents.",
		"---",
		"",
		"See [the reference guide](references/REFERENCE.md).",
		"",
		"Run `scripts/extract.py` against the input.",
	}, "\n")

	manifest, err := parseSkillManifest(content)
	require.NoError(t, err)
	require.True(t, manifest.SpecValid)
	require.Empty(t, manifest.ValidationErrors, "referencing supporting files is spec-valid")
	require.Equal(t, []skillResourceReference{
		{Path: "references/REFERENCE.md", Kind: skillResourceKindReference},
		{Path: "scripts/extract.py", Kind: skillResourceKindScript},
	}, manifest.ResourceReferences)
}

func TestParseSkillManifestIgnoresFrontmatterPaths(t *testing.T) {
	t.Parallel()

	content := strings.Join([]string{
		"---",
		"name: pdf-processing",
		"description: Extracts text from PDFs. Use when handling PDF documents.",
		"license: Proprietary. references/LICENSE.txt has complete terms",
		"---",
		"",
		"No supporting files here.",
	}, "\n")

	manifest, err := parseSkillManifest(content)
	require.NoError(t, err)
	require.Empty(t, manifest.ResourceReferences)
}
