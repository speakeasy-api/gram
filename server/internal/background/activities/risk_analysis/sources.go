package risk_analysis

import "github.com/speakeasy-api/gram/server/internal/gitleaks"

const (
	// SourceGitleaks is the policy source value for secret scanning.
	SourceGitleaks = gitleaks.Source
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
