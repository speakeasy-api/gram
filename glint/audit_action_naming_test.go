package glint

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAuditActionNaming(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, newAuditActionNamingAnalyzer(auditActionNamingSettings{}), "auditactionnaming/server/internal/audit")
}

func TestAuditActionNamingSkipsNonAuditPackage(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, newAuditActionNamingAnalyzer(auditActionNamingSettings{}), "notaudit")
}

func TestBuildAnalyzersSkipsDisabledAuditActionNaming(t *testing.T) {
	t.Parallel()

	p := disabledAllRulesPlugin()
	p.settings.Rules.AuditActionNaming.Disabled = false

	analyzers, err := p.BuildAnalyzers()
	require.NoError(t, err)
	require.Len(t, analyzers, 1)
	require.Equal(t, auditActionNamingAnalyzer, analyzers[0].Name)
}
