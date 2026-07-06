package risk_analysis

import (
	"github.com/speakeasy-api/gram/server/internal/risk/presetlib"
)

// dropBuiltinFalsePositives removes findings that the built-in preset library
// classifies as known false positives, keying on each finding's source, rule id
// and matched value. A finding is dropped when lib.Reason returns a non-empty
// catalog reason; an empty reason means it is a real finding and is retained. A
// nil library (feature not wired) drops nothing.
func dropBuiltinFalsePositives(lib *presetlib.Library, findings []Finding) []Finding {
	if lib == nil || len(findings) == 0 {
		return findings
	}
	out := make([]Finding, 0)
	for _, f := range findings {
		m := presetlib.Match{Source: f.Source, RuleID: f.RuleID, Value: f.Match}
		if lib.Reason(m) != "" {
			continue
		}
		out = append(out, f)
	}
	return out
}
