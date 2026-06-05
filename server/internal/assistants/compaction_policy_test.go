package assistants

import (
	"encoding/json"
	"testing"
)

func TestCompactionPolicy_Validate_RejectsEmpty(t *testing.T) {
	t.Parallel()
	var p CompactionPolicy
	if err := p.Validate(); err == nil {
		t.Fatal("empty policy must fail validation")
	}
}

func TestCompactionPolicy_Validate_RejectsMultipleVariants(t *testing.T) {
	t.Parallel()
	p := NewOnTurnEndPolicy()
	p.Threshold = &compactionThreshold{Percent: 60}
	if err := p.Validate(); err == nil {
		t.Fatal("policy with two variants set must fail validation")
	}
}

func TestCompactionPolicy_Validate_RejectsOutOfRangePercent(t *testing.T) {
	t.Parallel()
	for _, percent := range []uint8{0, 101, 200, 255} {
		p := NewThresholdPolicy(percent)
		if err := p.Validate(); err == nil {
			t.Fatalf("percent=%d must fail validation", percent)
		}
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
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if got := string(b); got != tc.wantJSON {
				t.Fatalf("wire shape mismatch:\n  got  %s\n  want %s", got, tc.wantJSON)
			}
			var round CompactionPolicy
			if err := json.Unmarshal(b, &round); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !tc.wantSetFn(round) {
				t.Fatalf("round-tripped policy lost its variant: %+v", round)
			}
			if err := round.Validate(); err != nil {
				t.Fatalf("round-tripped policy must validate: %v", err)
			}
		})
	}
}

func TestCompactionPolicyFor(t *testing.T) {
	t.Parallel()

	cron := compactionPolicyFor(sourceKindCron)
	if cron.OnTurnEnd == nil {
		t.Fatalf("cron must map to OnTurnEnd, got %+v", cron)
	}

	for _, kind := range []string{sourceKindSlack, sourceKindWake, sourceKindDashboard, "unknown-future-kind", ""} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			p := compactionPolicyFor(kind)
			if p.Threshold == nil || p.Threshold.Percent != 60 {
				t.Fatalf("source kind %q must map to Threshold{60}, got %+v", kind, p)
			}
		})
	}
}
