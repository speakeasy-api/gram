package glint

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAuditEventURNNaming(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, newAuditEventURNNamingAnalyzer(auditEventURNNamingSettings{}), "auditeventurnnaming/server/internal/audit")
}

func TestAuditEventURNNamingSkipsNonAuditPackage(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, newAuditEventURNNamingAnalyzer(auditEventURNNamingSettings{}), "notaudit")
}

func TestBuildAnalyzersSkipsDisabledAuditEventURNNaming(t *testing.T) {
	t.Parallel()

	p := disabledAllRulesPlugin()
	p.settings.Rules.AuditEventURNNaming.Disabled = false

	analyzers, err := p.BuildAnalyzers()
	require.NoError(t, err)
	require.Len(t, analyzers, 1)
	require.Equal(t, auditEventURNNamingAnalyzer, analyzers[0].Name)
}
