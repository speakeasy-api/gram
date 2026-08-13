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
