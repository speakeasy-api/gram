package risk_analysis

import (
	"github.com/speakeasy-api/gram/server/internal/risk/presetlib"
)

// dropBuiltinFalsePositives removes findings that the built-in preset catalog
// classifies as known false positives, keying on each finding's source, rule id
// and matched value. A finding is dropped when presetlib.Reason returns a
// non-empty catalog reason; an empty reason means it is a real finding and is
// retained. The returned slice is filtered in place to stay allocation-lean.
func dropBuiltinFalsePositives(findings []Finding) []Finding {
	if len(findings) == 0 {
		return findings
	}
	out := findings[:0]
	for _, f := range findings {
		m := presetlib.Match{Source: f.Source, RuleID: f.RuleID, Value: f.Match}
		if presetlib.Reason(m) != "" {
			continue
		}
		out = append(out, f)
	}
	return out
}
