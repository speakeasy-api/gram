package loopsync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManifestValidate_RequiresManagedNameDerivedFromKey(t *testing.T) {
	t.Parallel()

	manifest := validManifest(t)
	spec := manifest.Templates["example_notice"]
	spec.ManagedName = "gram.transactional.v2.different_notice"
	manifest.Templates["example_notice"] = spec

	err := manifest.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "want \"gram.transactional.v2.example_notice\"")
}

func TestManifestValidate_RejectsDuplicateVariables(t *testing.T) {
	t.Parallel()

	manifest := validManifest(t)
	spec := manifest.Templates["example_notice"]
	spec.Variables = []string{"resource_name", "resource_name"}
	manifest.Templates["example_notice"] = spec

	err := manifest.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "declares variable \"resource_name\" more than once")
}

func TestLoadManifest_RejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	manifest := validManifest(t)
	data, err := json.Marshal(manifest)
	require.NoError(t, err)
	data = append(data, []byte(`{"unexpected":true}`)...)
	path := filepath.Join(manifest.Dir, "manifest.json")
	require.NoError(t, os.WriteFile(path, data, 0o600))

	_, err = LoadManifest(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "trailing data")
}

func TestManifestValidate_RejectsColumnsGapBelowProviderMinimum(t *testing.T) {
	t.Parallel()

	manifest := validManifest(t)
	lmx := `<Columns gap="0"><ColumnItem><Paragraph>{data.resource_name}</Paragraph></ColumnItem></Columns>`
	require.NoError(t, os.WriteFile(filepath.Join(manifest.Dir, "example_notice.lmx"), []byte(lmx), 0o600))

	err := manifest.Validate()
	require.ErrorContains(t, err, `Columns attribute "gap" must be an integer between 12 and 150 (got "0")`)
}

func TestManifestValidate_RejectsParagraphFontSizeBelowProviderMinimum(t *testing.T) {
	t.Parallel()

	manifest := validManifest(t)
	lmx := `<Paragraph fontSize="10">{data.resource_name}</Paragraph>`
	require.NoError(t, os.WriteFile(filepath.Join(manifest.Dir, "example_notice.lmx"), []byte(lmx), 0o600))

	err := manifest.Validate()
	require.ErrorContains(t, err, `Paragraph attribute "fontSize" must be an integer between 12 and 64 (got "10")`)
}

func TestManifestValidate_RejectsNonIntegerParagraphFontSize(t *testing.T) {
	t.Parallel()

	manifest := validManifest(t)
	lmx := `<Paragraph fontSize="small">{data.resource_name}</Paragraph>`
	require.NoError(t, os.WriteFile(filepath.Join(manifest.Dir, "example_notice.lmx"), []byte(lmx), 0o600))

	err := manifest.Validate()
	require.ErrorContains(t, err, `Paragraph attribute "fontSize" must be an integer between 12 and 64 (got "small")`)
}

func TestManifestValidate_RejectsColumnsGapAboveProviderMaximum(t *testing.T) {
	t.Parallel()

	manifest := validManifest(t)
	lmx := `<Columns gap="151"><ColumnItem><Paragraph>{data.resource_name}</Paragraph></ColumnItem></Columns>`
	require.NoError(t, os.WriteFile(filepath.Join(manifest.Dir, "example_notice.lmx"), []byte(lmx), 0o600))

	err := manifest.Validate()
	require.ErrorContains(t, err, `Columns attribute "gap" must be an integer between 12 and 150 (got "151")`)
}

func TestManifestValidate_RejectsNonIntegerColumnsGap(t *testing.T) {
	t.Parallel()

	manifest := validManifest(t)
	lmx := `<Columns gap="none"><ColumnItem><Paragraph>{data.resource_name}</Paragraph></ColumnItem></Columns>`
	require.NoError(t, os.WriteFile(filepath.Join(manifest.Dir, "example_notice.lmx"), []byte(lmx), 0o600))

	err := manifest.Validate()
	require.ErrorContains(t, err, `Columns attribute "gap" must be an integer between 12 and 150 (got "none")`)
}

func TestManifestValidate_RejectsParagraphFontSizeAboveProviderMaximum(t *testing.T) {
	t.Parallel()

	manifest := validManifest(t)
	lmx := `<Paragraph fontSize="65">{data.resource_name}</Paragraph>`
	require.NoError(t, os.WriteFile(filepath.Join(manifest.Dir, "example_notice.lmx"), []byte(lmx), 0o600))

	err := manifest.Validate()
	require.ErrorContains(t, err, `Paragraph attribute "fontSize" must be an integer between 12 and 64 (got "65")`)
}

func TestManifestValidate_AcceptsProviderAttributeBoundaries(t *testing.T) {
	t.Parallel()

	manifest := validManifest(t)
	lmx := `<Columns gap="12"><ColumnItem><Paragraph fontSize="12">{data.resource_name}</Paragraph></ColumnItem></Columns><Columns gap="150"><ColumnItem><Paragraph fontSize="64">Example</Paragraph></ColumnItem></Columns>`
	require.NoError(t, os.WriteFile(filepath.Join(manifest.Dir, "example_notice.lmx"), []byte(lmx), 0o600))

	require.NoError(t, manifest.Validate())
}

func TestManifestValidate_AcceptsOmittedProviderAttributes(t *testing.T) {
	t.Parallel()

	manifest := validManifest(t)
	lmx := `<Columns><ColumnItem><Paragraph>{data.resource_name}</Paragraph></ColumnItem></Columns>`
	require.NoError(t, os.WriteFile(filepath.Join(manifest.Dir, "example_notice.lmx"), []byte(lmx), 0o600))

	require.NoError(t, manifest.Validate())
}

func TestManifestValidate_ValidatesNestedUnregisteredLMXFiles(t *testing.T) {
	t.Parallel()

	manifest := validManifest(t)
	nestedDir := filepath.Join(manifest.Dir, "starters")
	require.NoError(t, os.Mkdir(nestedDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(nestedDir, "transactional_base.lmx"), []byte(`<Paragraph fontSize="10">Example</Paragraph>`), 0o600))

	err := manifest.Validate()
	require.ErrorContains(t, err, "transactional_base.lmx")
	require.ErrorContains(t, err, `Paragraph attribute "fontSize" must be an integer between 12 and 64 (got "10")`)
}

func validManifest(t *testing.T) *Manifest {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "example_notice.lmx"), []byte(`<Paragraph>{data.resource_name}</Paragraph>`), 0o600))

	return &Manifest{
		Version: 1,
		Defaults: MessageDefaults{
			FromName:     "Speakeasy",
			FromEmail:    "gram",
			ReplyToEmail: "gram@speakeasy.com",
		},
		Templates: map[string]TemplateSpec{
			"example_notice": {
				ManagedName:     "gram.transactional.v2.example_notice",
				Subject:         "Example notice",
				PreviewText:     "Review this notice.",
				Source:          "example_notice.lmx",
				Variables:       []string{"resource_name"},
				UnusedVariables: nil,
				LMX:             "",
				SourceVariables: nil,
			},
		},
		Dir: dir,
	}
}
