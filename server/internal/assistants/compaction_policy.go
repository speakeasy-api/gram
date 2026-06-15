package assistants

import (
	"errors"
	"fmt"
)

// CompactionPolicy is the sealed sum type the server sends to the runner to
// tell it when to compact a thread. Exactly one of the variant pointers is
// non-nil. Construct via NewThresholdPolicy / NewOnTurnEndPolicy /
// NewOffPolicy — the variant struct types are unexported so callers cannot
// build malformed policies.
type CompactionPolicy struct {
	Threshold *compactionThreshold `json:"threshold,omitempty"`
	OnTurnEnd *compactionOnTurnEnd `json:"on_turn_end,omitempty"`
	Off       *compactionOff       `json:"off,omitempty"`
}

type compactionThreshold struct {
	Percent uint8 `json:"percent"`
}

type compactionOnTurnEnd struct{}

type compactionOff struct{}

// Sealed sum: each constructor sets exactly one variant pointer and leaves
// the others nil. The explicit nils satisfy exhaustruct without disabling it.
func NewThresholdPolicy(percent uint8) CompactionPolicy {
	return CompactionPolicy{Threshold: &compactionThreshold{Percent: percent}, OnTurnEnd: nil, Off: nil}
}

func NewOnTurnEndPolicy() CompactionPolicy {
	return CompactionPolicy{Threshold: nil, OnTurnEnd: &compactionOnTurnEnd{}, Off: nil}
}

func NewOffPolicy() CompactionPolicy {
	return CompactionPolicy{Threshold: nil, OnTurnEnd: nil, Off: &compactionOff{}}
}

// Validate asserts exactly one variant is set and that any variant-specific
// fields are within range. Called before serialisation so a malformed policy
// cannot leave the server.
func (p CompactionPolicy) Validate() error {
	set := 0
	if p.Threshold != nil {
		set++
	}
	if p.OnTurnEnd != nil {
		set++
	}
	if p.Off != nil {
		set++
	}
	switch set {
	case 0:
		return errors.New("compaction policy: no variant set")
	case 1:
		// fallthrough to per-variant checks
	default:
		return fmt.Errorf("compaction policy: %d variants set, want exactly 1", set)
	}
	if p.Threshold != nil && (p.Threshold.Percent == 0 || p.Threshold.Percent > 100) {
		return fmt.Errorf("compaction policy threshold: percent must be 1..=100, got %d", p.Threshold.Percent)
	}
	return nil
}

// compactionPolicyFor maps a thread source kind to its compaction policy.
// Cron threads fire every ≥24h, so the inter-fire gap means the prompt cache
// never warms across fires and the threshold rarely trips before the chat
// ends — compact at every turn end instead. All other source kinds use the
// 60% threshold.
func compactionPolicyFor(sourceKind string) CompactionPolicy {
	switch sourceKind {
	case sourceKindCron:
		return NewOnTurnEndPolicy()
	default:
		return NewThresholdPolicy(60)
	}
}
