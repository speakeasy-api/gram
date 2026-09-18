package metering

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefinition_IsRiskScanner(t *testing.T) {
	t.Parallel()

	var zero Definition
	stale := RiskGitleaks()
	stale.version++

	cases := []struct {
		name string
		def  Definition
		want bool
	}{
		{name: "zero definition", def: zero, want: false},
		{name: "registered non-risk meter", def: AgentSessionStorage(), want: false},
		{name: "unregistered risk id", def: riskDefinition("gram.risk.scan.unknown"), want: false},
		{name: "registered risk id at a stale version", def: stale, want: false},
		{name: "registered risk meter", def: RiskLLMAnalyzer(), want: true},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, tc.def.IsRiskScanner(), tc.name)
	}
}
