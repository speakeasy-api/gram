package risk_analysis

import (
	"github.com/speakeasy-api/gram/server/internal/risk/presetlib"
)

// IsBuiltinFalsePositive reports whether a finding matches the built-in preset library.
func IsBuiltinFalsePositive(lib *presetlib.Library, source, ruleID, value string) bool {
	if lib == nil {
		return false
	}
	return lib.Reason(presetlib.Match{Source: source, RuleID: ruleID, Value: value}) != ""
}
