package glint

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAuditEventTypedSnapshot(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, newAuditEventTypedSnapshotAnalyzer(auditEventTypedSnapshotSettings{}), "auditeventtypedsnapshot/server/internal/audit")
}

func TestAuditEventTypedSnapshotSkipsNonAuditPackage(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, newAuditEventTypedSnapshotAnalyzer(auditEventTypedSnapshotSettings{}), "notaudit")
}

func TestBuildAnalyzersSkipsDisabledAuditEventTypedSnapshot(t *testing.T) {
	t.Parallel()

	p := disabledAllRulesPlugin()
	p.settings.Rules.AuditEventTypedSnapshot.Disabled = false

	analyzers, err := p.BuildAnalyzers()
	require.NoError(t, err)
	require.Len(t, analyzers, 1)
	require.Equal(t, auditEventTypedSnapshotAnalyzer, analyzers[0].Name)
}
