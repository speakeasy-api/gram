package loops_test

import (
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/email"
	"github.com/speakeasy-api/gram/server/internal/email/loops"
)

const (
	canonicalGradientImage = `<Image src="https://images.vialoops.com/clydgspni01t0bsa10jmd46rt/cmshilgvx01u30j6t4t0211tl.png" alt="" width="536" paddingTop="12" paddingBottom="20" />`
	canonicalLockupImage   = `<Image src="https://images.vialoops.com/clydgspni01t0bsa10jmd46rt/cmt7eueee05e20izu0frdx1jq.png" alt="Speakeasy" width="160" align="left" paddingBottom="28" />`
)

func TestManifestMatchesApplicationTemplateContract(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	manifest, err := loops.LoadManifest(filepath.Join(filepath.Dir(filename), "manifest.json"))
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
		require.Equal(t, 1, strings.Count(spec.LMX, canonicalLockupImage), "canonical lockup for %q", tmpl.Key())
		require.Equal(t, 1, strings.Count(spec.LMX, canonicalGradientImage), "canonical gradient line for %q", tmpl.Key())
		require.NotContains(t, spec.Subject, "Gram", "recipient-visible subject for %q", tmpl.Key())
		require.NotContains(t, spec.PreviewText, "Gram", "recipient-visible preview for %q", tmpl.Key())
		require.NotContains(t, spec.LMX, "Gram", "recipient-visible copy for %q", tmpl.Key())
	}
}
