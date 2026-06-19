package assistants

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompactionPolicy_Validate_RejectsEmpty(t *testing.T) {
	t.Parallel()
	var p CompactionPolicy
	require.Error(t, p.Validate(), "empty policy must fail validation")
}

func TestCompactionPolicy_Validate_RejectsMultipleVariants(t *testing.T) {
	t.Parallel()
	p := NewOnTurnEndPolicy()
	p.Threshold = &compactionThreshold{Percent: 60}
	require.Error(t, p.Validate(), "policy with two variants set must fail validation")
}

func TestCompactionPolicy_Validate_RejectsOutOfRangePercent(t *testing.T) {
	t.Parallel()
	for _, percent := range []uint8{0, 101, 200, 255} {
		p := NewThresholdPolicy(percent)
		require.Errorf(t, p.Validate(), "percent=%d must fail validation", percent)
	}
}

func TestCompactionPolicy_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		policy    CompactionPolicy
		wantJSON  string
		wantSetFn func(CompactionPolicy) bool
	}{
		{
			name:      "threshold",
			policy:    NewThresholdPolicy(60),
			wantJSON:  `{"threshold":{"percent":60}}`,
			wantSetFn: func(p CompactionPolicy) bool { return p.Threshold != nil && p.Threshold.Percent == 60 },
		},
		{
			name:      "on_turn_end",
			policy:    NewOnTurnEndPolicy(),
			wantJSON:  `{"on_turn_end":{}}`,
			wantSetFn: func(p CompactionPolicy) bool { return p.OnTurnEnd != nil },
		},
		{
			name:      "off",
			policy:    NewOffPolicy(),
			wantJSON:  `{"off":{}}`,
			wantSetFn: func(p CompactionPolicy) bool { return p.Off != nil },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b, err := json.Marshal(tc.policy)
			require.NoError(t, err)
			require.Equal(t, tc.wantJSON, string(b), "wire shape must match")
			var round CompactionPolicy
			require.NoError(t, json.Unmarshal(b, &round))
			require.True(t, tc.wantSetFn(round), "round-tripped policy must preserve variant: %+v", round)
			require.NoError(t, round.Validate(), "round-tripped policy must validate")
		})
	}
}

func TestCompactionPolicyFor(t *testing.T) {
	t.Parallel()

	cron := compactionPolicyFor(sourceKindCron)
	require.NotNil(t, cron.OnTurnEnd, "cron must map to OnTurnEnd, got %+v", cron)

	for _, kind := range []string{sourceKindSlack, sourceKindWake, sourceKindDashboard, "unknown-future-kind", ""} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			p := compactionPolicyFor(kind)
			require.NotNil(t, p.Threshold, "source kind %q must map to Threshold, got %+v", kind, p)
			require.EqualValues(t, 60, p.Threshold.Percent, "source kind %q must map to Threshold{60}", kind)
		})
	}
}
