package risk_analysis

import (
	"github.com/speakeasy-api/gram/server/internal/scanners/accountidentity"
	"github.com/speakeasy-api/gram/server/internal/scanners/clidestructive"
	"github.com/speakeasy-api/gram/server/internal/scanners/gitleaks"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
)

const (
	// SourceGitleaks is the policy source value for secret scanning. Its default
	// ruleset is extended with AWS secret-access-key and session-token rules
	// (see the gitleaks package), so all three AWS credential flavors surface
	// under this one source.
	SourceGitleaks = gitleaks.Source
	// SourceCLIDestructive is the policy source value that flags tool calls
	// whose arguments contain a curated destructive CLI command pattern
	// (rm -rf, git push --force, DROP TABLE, ...). It is content-driven
	// instead of annotation-driven and applies to native tools (Bash,
	// run_terminal_cmd) as well as MCP-routed calls whose arguments happen to
	// carry a destructive payload.
	SourceCLIDestructive = clidestructive.Source
	// SourceAccountIdentity is the policy source value flagging sessions
	// authenticated with a non-corporate AI account. Unlike the content
	// scanners it inspects the chat's account attribution (personal-account
	// tracking data on user_accounts), not the message text.
	SourceAccountIdentity = accountidentity.Source
	// SourcePromptInjection is the policy source value for prompt injection scanning.
	SourcePromptInjection = promptinjection.Source
	// SourceNone marks the sentinel row for an analyzed message with no findings.
	SourceNone = "none"

	// PolicyTypeStandard evaluates configured detector sources.
	PolicyTypeStandard = "standard"
	// PolicyTypePromptBased evaluates a natural-language prompt with the judge.
	PolicyTypePromptBased = "prompt_based"
)

type sourceSet struct {
	values map[string]struct{}
}

func newSourceSet(sources []string) sourceSet {
	if len(sources) == 0 {
		return sourceSet{values: map[string]struct{}{}}
	}
	values := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if source == "" {
			continue
		}
		values[source] = struct{}{}
	}
	return sourceSet{values: values}
}

func (s sourceSet) Has(source string) bool {
	_, ok := s.values[source]
	return ok
}
