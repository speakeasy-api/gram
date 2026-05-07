package glint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// disabledAllRulesPlugin returns a plugin instance with every rule disabled.
// Tests can flip individual rules back on to assert per-rule registration.
func disabledAllRulesPlugin() *plugin {
	return &plugin{
		settings: settings{
			Rules: ruleSettings{
				NoAnonymousDefer:           noAnonymousDeferSettings{Disabled: true},
				EnforceO11yConventions:     enforceO11yConventionsSettings{Disabled: true},
				ServiceHasServiceAssertion: serviceHasServiceAssertionSettings{Disabled: true},
				ServiceHasAutherAssertion:  serviceHasAutherAssertionSettings{Disabled: true},
				ServiceHasAttachFunc:       serviceHasAttachFuncSettings{Disabled: true},
				NoRepoFieldsInService:      noRepoFieldsInServiceSettings{Disabled: true},
				AuditEventTypedSnapshot:    auditEventTypedSnapshotSettings{Disabled: true},
				AuditEventURNNaming:        auditEventURNNamingSettings{Disabled: true},
				AuditEventURNTyping:        auditEventURNTypingSettings{Disabled: true},
				NoDirectChatMessageInsert:  noDirectChatMessageInsertSettings{Disabled: true},
				NoSqlErrNoRows:             noSqlErrNoRowsSettings{Disabled: true},
				NoTestingRawSql:            noTestingRawSqlSettings{Disabled: true},
			},
		},
	}
}

func TestBuildAnalyzersAllDisabled(t *testing.T) {
	t.Parallel()

	p := disabledAllRulesPlugin()
	analyzers, err := p.BuildAnalyzers()
	require.NoError(t, err)
	require.Empty(t, analyzers)
}

func TestBuildAnalyzersAllEnabled(t *testing.T) {
	t.Parallel()

	p := &plugin{}
	analyzers, err := p.BuildAnalyzers()
	require.NoError(t, err)
	require.Len(t, analyzers, 12)
}
