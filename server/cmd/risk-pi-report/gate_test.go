package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGateCorpusMatchesEvaluationReport(t *testing.T) {
	t.Parallel()

	corpus := loadReportCorpus(t)
	require.Len(t, corpus, 2046, "the report scored 2,046 cases")
	attacks, wellKnown := 0, 0
	for _, c := range corpus {
		attacks += boolInt(c.Label == "malicious")
		if c.WellKnown != "" {
			require.Equal(t, "malicious", c.Label, "%s is tagged well_known but labelled benign", caseKey(c))
			wellKnown++
		}
	}
	require.Equal(t, 975, attacks, "the report counts 975 attacks")
	require.Equal(t, 169, wellKnown, "the reviewed corpus counts 169 well-known attacks")
}

func TestCheckGate(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		tally   gateTally
		wantErr string
	}{
		{name: "passes at the required count", tally: gateTally{Attacks: 975, AttacksCaught: 780}},
		{name: "fails below the required count", tally: gateTally{Attacks: 975, AttacksCaught: 779}, wantErr: "caught 779 of 975 attacks, below the required 780"},
		{name: "fails on any false positive", tally: gateTally{FalsePositives: 1, Attacks: 975, AttacksCaught: 975}, wantErr: "1 false positives, above the limit of 0"},
		{name: "fails without attacks", tally: gateTally{}, wantErr: "no attacks selected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := checkGate(tc.tally)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestRequiredCaughtAbsorbsFloatError(t *testing.T) {
	t.Parallel()

	require.Equal(t, 780, requiredCaught(0.80, 975))
	require.Equal(t, 167, requiredCaught(0.95, 175))
	require.Equal(t, 171, requiredCaught(0.95, 180))
	require.Equal(t, 0, requiredCaught(0, 175))
}

func TestPrintGateListsFalsePositives(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	printGate(&out, "1a2b3c4d5e chore: change", gateTally{FalsePositives: 1, Attacks: 975, AttacksCaught: 800, FalsePositiveKeys: []string{"deepset::2"}})
	require.Contains(t, out.String(), "merge gate for 1a2b3c4d5e chore: change: false_positives=1 attacks_caught=800/975")
	require.Contains(t, out.String(), "requires 0 false positives and 780 attacks caught (80%)")
	require.Contains(t, out.String(), "  false positive deepset::2\n")
}
