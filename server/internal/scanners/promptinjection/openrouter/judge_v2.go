package openrouter

import "sort"

const (
	DirectiveNone                    = "none"
	DirectiveInstructionOverride     = "instruction_override"
	DirectiveGuardedSecretExtraction = "guarded_secret_extraction"
	DirectiveExternalExfiltration    = "external_exfiltration"

	TargetGuardedAgent = "guarded_agent"
	TargetOtherContext = "other_context"
	TargetUnclear      = "unclear"
	TargetNone         = "none"
)

// Verdict is the model's typed semantic evidence. It intentionally excludes
// model confidence and enforcement policy.
type Verdict struct {
	DirectiveKind string `json:"directive_kind"`
	Target        string `json:"target"`
	Operational   bool   `json:"operational"`
	Rationale     string `json:"rationale"`
}

// IsInjection is the only typed detection predicate. The existing risk-policy
// layer independently decides whether a resulting finding blocks or surfaces.
func IsInjection(verdict Verdict) bool {
	if !verdict.Operational || verdict.DirectiveKind == DirectiveNone {
		return false
	}
	return verdict.Target == TargetGuardedAgent || verdict.Target == TargetUnclear
}

// ValidVerdict rejects syntactically valid JSON that violates the typed
// vocabulary or its cross-field invariants. Production and the evaluator both
// treat it as a malformed safe vote.
func ValidVerdict(verdict Verdict) bool {
	switch verdict.DirectiveKind {
	case DirectiveNone, DirectiveInstructionOverride, DirectiveGuardedSecretExtraction, DirectiveExternalExfiltration:
	default:
		return false
	}
	switch verdict.Target {
	case TargetGuardedAgent, TargetOtherContext, TargetUnclear, TargetNone:
	default:
		return false
	}
	if verdict.DirectiveKind == DirectiveNone {
		return verdict.Target == TargetNone && !verdict.Operational
	}
	return verdict.Target != TargetNone
}

// VerdictSchema is the strict evidence contract shared by production and the
// evaluator. Enforcement is deliberately absent.
func VerdictSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"directive_kind": map[string]any{
				"type": "string",
				"enum": []string{DirectiveNone, DirectiveInstructionOverride, DirectiveGuardedSecretExtraction, DirectiveExternalExfiltration},
			},
			"target": map[string]any{
				"type": "string",
				"enum": []string{TargetGuardedAgent, TargetOtherContext, TargetUnclear, TargetNone},
			},
			"operational": map[string]any{"type": "boolean"},
			"rationale":   map[string]any{"type": "string"},
		},
		"required":             []string{"directive_kind", "target", "operational", "rationale"},
		"additionalProperties": false,
	}
}

// Stabilized is the code-owned typed result. Vote fields exist only for the
// optional multi-sample override.
type Stabilized struct {
	IsInjection   bool
	DirectiveKind string
	Target        string
	Operational   bool
	PositiveVotes int
	Samples       int
	Unanimous     bool
	Rationale     string
}

// StabilizeSingle applies the typed predicate directly without aggregation.
func StabilizeSingle(verdict Verdict) Stabilized {
	if !IsInjection(verdict) {
		return Stabilized{
			IsInjection:   false,
			DirectiveKind: verdict.DirectiveKind,
			Target:        verdict.Target,
			Operational:   verdict.Operational,
			PositiveVotes: 0,
			Samples:       1,
			Unanimous:     false,
			Rationale:     "",
		}
	}
	return Stabilized{
		IsInjection:   true,
		DirectiveKind: verdict.DirectiveKind,
		Target:        verdict.Target,
		Operational:   verdict.Operational,
		PositiveVotes: 1,
		Samples:       1,
		Unanimous:     true,
		Rationale:     verdict.Rationale,
	}
}

// Aggregate applies a strict majority to the configured sample set. A zero
// Verdict represents an errored, timed-out, rate-limited, or malformed sample
// and therefore counts against the majority as a safe vote.
func Aggregate(verdicts []Verdict) Stabilized {
	positive := make([]Verdict, 0, len(verdicts))
	for _, verdict := range verdicts {
		if IsInjection(verdict) {
			positive = append(positive, verdict)
		}
	}

	positiveVotes := len(positive)
	majority := positiveVotes >= len(verdicts)/2+1
	if len(verdicts) == 0 || !majority {
		return Stabilized{
			IsInjection:   false,
			DirectiveKind: DirectiveNone,
			Target:        TargetNone,
			Operational:   false,
			PositiveVotes: positiveVotes,
			Samples:       len(verdicts),
			Unanimous:     len(verdicts) > 0 && positiveVotes == len(verdicts),
			Rationale:     "",
		}
	}

	kinds := make(map[string]int, len(positive))
	targets := make(map[string]int, len(positive))
	rationale := ""
	for _, verdict := range positive {
		kinds[verdict.DirectiveKind]++
		targets[verdict.Target]++
		if rationale == "" {
			rationale = verdict.Rationale
		}
	}

	return Stabilized{
		IsInjection:   true,
		DirectiveKind: modal(kinds, directiveRank),
		Target:        modal(targets, targetRank),
		Operational:   true,
		PositiveVotes: positiveVotes,
		Samples:       len(verdicts),
		Unanimous:     positiveVotes == len(verdicts),
		Rationale:     rationale,
	}
}

func modal(counts map[string]int, rank func(string) int) string {
	type entry struct {
		value string
		count int
	}
	entries := make([]entry, 0, len(counts))
	for value, count := range counts {
		entries = append(entries, entry{value: value, count: count})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count != entries[j].count {
			return entries[i].count > entries[j].count
		}
		return rank(entries[i].value) > rank(entries[j].value)
	})
	if len(entries) == 0 {
		return ""
	}
	return entries[0].value
}

func directiveRank(kind string) int {
	switch kind {
	case DirectiveExternalExfiltration:
		return 3
	case DirectiveGuardedSecretExtraction:
		return 2
	case DirectiveInstructionOverride:
		return 1
	default:
		return 0
	}
}

func targetRank(target string) int {
	switch target {
	case TargetGuardedAgent:
		return 2
	case TargetUnclear:
		return 1
	default:
		return 0
	}
}
