package glint

import (
	"fmt"

	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis"
)

const (
	pluginName = "glint"
)

func init() {
	register.Plugin(pluginName, New)
}

type settings struct {
	Rules ruleSettings `json:"rules"`
}

type ruleSettings struct {
	NoAnonymousDefer           noAnonymousDeferSettings           `json:"no-anonymous-defer"`
	EnforceO11yConventions     enforceO11yConventionsSettings     `json:"enforce-o11y-conventions"`
	ServiceHasServiceAssertion serviceHasServiceAssertionSettings `json:"service-has-service-assertion"`
	ServiceHasAutherAssertion  serviceHasAutherAssertionSettings  `json:"service-has-auther-assertion"`
	ServiceHasAttachFunc       serviceHasAttachFuncSettings       `json:"service-has-attach-func"`
	NoRepoFieldsInService      noRepoFieldsInServiceSettings      `json:"no-repo-fields-in-service"`
	AuditEventTypedSnapshot    auditEventTypedSnapshotSettings    `json:"audit-event-typed-snapshot"`
	AuditEventURNNaming        auditEventURNNamingSettings        `json:"audit-event-urn-naming"`
	AuditEventURNTyping        auditEventURNTypingSettings        `json:"audit-event-urn-typing"`
	NoDirectChatMessageInsert  noDirectChatMessageInsertSettings  `json:"no-direct-chat-message-insert"`
	NoSqlErrNoRows             noSqlErrNoRowsSettings             `json:"no-sql-err-no-rows"`
	NoTestingRawSql            noTestingRawSqlSettings            `json:"no-testing-raw-sql"`
}

type plugin struct {
	settings settings
}

func New(rawSettings any) (register.LinterPlugin, error) {
	s, err := register.DecodeSettings[settings](rawSettings)
	if err != nil {
		return nil, fmt.Errorf("decode settings: %w", err)
	}

	return &plugin{settings: s}, nil
}

func (p *plugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	analyzers := []*analysis.Analyzer{}
	if !p.settings.Rules.NoAnonymousDefer.Disabled {
		analyzers = append(analyzers, newNoAnonymousDeferAnalyzer(p.settings.Rules.NoAnonymousDefer))
	}
	if !p.settings.Rules.EnforceO11yConventions.Disabled {
		analyzers = append(analyzers, newEnforceO11yConventionsAnalyzer(p.settings.Rules.EnforceO11yConventions))
	}
	if !p.settings.Rules.ServiceHasServiceAssertion.Disabled {
		analyzers = append(analyzers, newServiceHasServiceAssertionAnalyzer(p.settings.Rules.ServiceHasServiceAssertion))
	}
	if !p.settings.Rules.ServiceHasAutherAssertion.Disabled {
		analyzers = append(analyzers, newServiceHasAutherAssertionAnalyzer(p.settings.Rules.ServiceHasAutherAssertion))
	}
	if !p.settings.Rules.ServiceHasAttachFunc.Disabled {
		analyzers = append(analyzers, newServiceHasAttachFuncAnalyzer(p.settings.Rules.ServiceHasAttachFunc))
	}
	if !p.settings.Rules.NoRepoFieldsInService.Disabled {
		analyzers = append(analyzers, newNoRepoFieldsInServiceAnalyzer(p.settings.Rules.NoRepoFieldsInService))
	}
	if !p.settings.Rules.AuditEventTypedSnapshot.Disabled {
		analyzers = append(analyzers, newAuditEventTypedSnapshotAnalyzer(p.settings.Rules.AuditEventTypedSnapshot))
	}
	if !p.settings.Rules.AuditEventURNNaming.Disabled {
		analyzers = append(analyzers, newAuditEventURNNamingAnalyzer(p.settings.Rules.AuditEventURNNaming))
	}
	if !p.settings.Rules.AuditEventURNTyping.Disabled {
		analyzers = append(analyzers, newAuditEventURNTypingAnalyzer(p.settings.Rules.AuditEventURNTyping))
	}
	if !p.settings.Rules.NoDirectChatMessageInsert.Disabled {
		analyzers = append(analyzers, newNoDirectChatMessageInsertAnalyzer(p.settings.Rules.NoDirectChatMessageInsert))
	}
	if !p.settings.Rules.NoSqlErrNoRows.Disabled {
		analyzers = append(analyzers, newNoSqlErrNoRowsAnalyzer(p.settings.Rules.NoSqlErrNoRows))
	}
	if !p.settings.Rules.NoTestingRawSql.Disabled {
		analyzers = append(analyzers, newNoTestingRawSqlAnalyzer(p.settings.Rules.NoTestingRawSql))
	}

	return analyzers, nil
}

func (p *plugin) GetLoadMode() string {
	return register.LoadModeTypesInfo
}
