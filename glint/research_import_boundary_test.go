package glint

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestResearchImportBoundary(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	// Testdata packages cannot carry the production import paths, so the
	// protected set is overridden to the fixture's "agent" package; the
	// sibling "outside" package proves the boundary does not leak.
	analyzer := newResearchImportBoundaryAnalyzer(researchImportBoundarySettings{
		Disabled: false,
		Packages: []string{"researchimportboundary/agent"},
	})
	analysistest.Run(t, testdata, analyzer, "researchimportboundary/...")
}

func TestResearchImportBoundaryDefaultsProtectProductionPackages(t *testing.T) {
	t.Parallel()

	require.Contains(t, researchImportBoundaryPackages, "github.com/speakeasy-api/gram/server/internal/mcpapproval/researchagent")
	require.Contains(t, researchImportBoundaryPackages, "github.com/speakeasy-api/gram/server/internal/platformtools/research")
	require.True(t, researchImportBoundaryViolated("github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"))
	require.True(t, researchImportBoundaryViolated("github.com/speakeasy-api/gram/server/internal/risk/chrepo"))
	require.True(t, researchImportBoundaryViolated("github.com/jackc/pgx/v5/pgxpool"))
	require.True(t, researchImportBoundaryViolated("github.com/ClickHouse/ch-go/proto"))
	require.False(t, researchImportBoundaryViolated("github.com/speakeasy-api/gram/server/internal/oops"))
}

func TestBuildAnalyzersSkipsDisabledResearchImportBoundary(t *testing.T) {
	t.Parallel()

	p := disabledAllRulesPlugin()
	p.settings.Rules.ResearchImportBoundary.Disabled = false

	analyzers, err := p.BuildAnalyzers()
	require.NoError(t, err)
	require.Len(t, analyzers, 1)
	require.Equal(t, researchImportBoundaryAnalyzer, analyzers[0].Name)
}
