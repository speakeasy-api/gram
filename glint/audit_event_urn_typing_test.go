package glint

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAuditEventURNTyping(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, newAuditEventURNTypingAnalyzer(auditEventURNTypingSettings{}), "auditeventurntyping/server/internal/audit")
}

func TestAuditEventURNTypingSkipsNonAuditPackage(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, newAuditEventURNTypingAnalyzer(auditEventURNTypingSettings{}), "notaudit")
}

func TestBuildAnalyzersSkipsDisabledAuditEventURNTyping(t *testing.T) {
	t.Parallel()

	p := disabledAllRulesPlugin()
	p.settings.Rules.AuditEventURNTyping.Disabled = false

	analyzers, err := p.BuildAnalyzers()
	require.NoError(t, err)
	require.Len(t, analyzers, 1)
	require.Equal(t, auditEventURNTypingAnalyzer, analyzers[0].Name)
}
