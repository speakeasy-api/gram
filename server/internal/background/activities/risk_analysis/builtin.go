package risk_analysis

import (
	"github.com/speakeasy-api/gram/server/internal/risk/presetlib"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

// IsBuiltinFalsePositive reports whether a finding matches the built-in preset library.
func IsBuiltinFalsePositive(lib *presetlib.Library, source, ruleID, value string) bool {
	if lib == nil {
		return false
	}
	return lib.Reason(presetlib.Match{Source: source, RuleID: ruleID, Value: value}) != ""
}

// dropBuiltinFalsePositives removes findings that the built-in preset library
// classifies as known false positives, keying on each finding's source, rule id
// and matched value. A finding is dropped when lib.Reason returns a non-empty
// catalog reason; an empty reason means it is a real finding and is retained. A
// nil library (feature not wired) drops nothing.
func dropBuiltinFalsePositives(lib *presetlib.Library, findings []scanners.Finding) []scanners.Finding {
	if lib == nil || len(findings) == 0 {
		return findings
	}
	out := make([]scanners.Finding, 0, len(findings))
	for _, f := range findings {
		if IsBuiltinFalsePositive(lib, f.Source, f.RuleID, f.Match) {
			continue
		}
		out = append(out, f)
	}
	return out
}
