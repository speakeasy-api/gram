package risk_analysis

import "context"

// SourcePresidio is the source label written on every risk_results row
// produced by the Presidio path, including dead-letter sentinels.
const SourcePresidio = "presidio"

// DeadLetterRuleID is set on the synthetic Finding emitted when a message
// permanently fails analysis after exhausting the retry budget. buildRows
// uses it as the rule_id for the dead-letter row.
const DeadLetterRuleID = "presidio.dead_letter"

// PIIScanner detects personally identifiable information in text.
type PIIScanner interface {
	// AnalyzeBatch sends multiple texts to the PII analyzer and returns
	// findings for each. The outer slice is indexed by input position.
	// When entities is non-empty, only those entity types are detected.
	//
	// Permanent per-message failures surface as a single Finding with
	// DeadLetterReason populated rather than as an error; the returned
	// error is non-nil only on outer-ctx cancellation.
	AnalyzeBatch(ctx context.Context, texts []string, entities []string, onProgress func()) ([][]Finding, error)
}

// StubPIIScanner is a no-op implementation for environments without Presidio.
type StubPIIScanner struct{}

func (s *StubPIIScanner) AnalyzeBatch(_ context.Context, texts []string, _ []string, _ func()) ([][]Finding, error) {
	return make([][]Finding, len(texts)), nil
}
