package loops_test

import (
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/email"
	"github.com/speakeasy-api/gram/server/internal/email/loopsync"
)

const (
	canonicalSpectrumImage = `<Image src="https://images.vialoops.com/clydgspni01t0bsa10jmd46rt/cmsrzwke702cu0j3bz2gats4u.png" width="600" />`
	canonicalLogoImage     = `<Image src="https://images.vialoops.com/clydgspni01t0bsa10jmd46rt/cmsrzv81y00z60i1dmtf9twha.png" width="28" />`
)

func TestManifestMatchesApplicationTemplateContract(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	manifest, err := loopsync.LoadManifest(filepath.Join(filepath.Dir(filename), "manifest.json"))
	require.NoError(t, err)
	require.Len(t, manifest.Templates, len(email.RegisteredTemplates))

	for _, tmpl := range email.RegisteredTemplates {
		spec, exists := manifest.Templates[string(tmpl.Key())]
		require.Truef(t, exists, "manifest missing %q", tmpl.Key())
		variables := make([]string, 0, len(tmpl.Variables()))
		for variable := range tmpl.Variables() {
			variables = append(variables, variable)
		}
		slices.Sort(variables)
		declared := slices.Clone(spec.Variables)
		slices.Sort(declared)
		require.Equal(t, variables, declared, "variable contract for %q", tmpl.Key())
		require.Equal(t, 1, strings.Count(spec.LMX, canonicalSpectrumImage), "canonical spectrum rail for %q", tmpl.Key())
		require.Equal(t, 1, strings.Count(spec.LMX, canonicalLogoImage), "canonical logo mark for %q", tmpl.Key())
	}
}
