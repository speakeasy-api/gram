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
// model confidence: consensus support is the only score used by the redesign.
type Verdict struct {
	DirectiveKind string `json:"directive_kind"`
	Target        string `json:"target"`
	Operational   bool   `json:"operational"`
	Rationale     string `json:"rationale"`
}

// IsInjection is the only detection predicate. Severity and action never
// suppress an otherwise eligible typed detection.
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
// evaluator. Detection, severity, and action are deliberately absent.
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

// Stabilized is the code-owned consensus result. Samples always includes
// failed and malformed calls because each is an explicit safe vote.
type Stabilized struct {
	IsInjection   bool
	DirectiveKind string
	Target        string
	PositiveVotes int
	Samples       int
	Unanimous     bool
	Rationale     string
}

// Aggregate applies a strict majority to the configured sample set. A zero
// Verdict represents an errored, timed-out, rate-limited, or malformed sample
// and therefore counts against consensus as a safe vote.
func Aggregate(verdicts []Verdict) Stabilized {
	positive := make([]Verdict, 0, len(verdicts))
	for _, verdict := range verdicts {
		if IsInjection(verdict) {
			positive = append(positive, verdict)
		}
	}

	positiveVotes := len(positive)
	consensus := positiveVotes >= len(verdicts)/2+1
	if len(verdicts) == 0 || !consensus {
		return Stabilized{
			IsInjection:   false,
			DirectiveKind: DirectiveNone,
			Target:        TargetNone,
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

type Severity string

const (
	SeverityNone   Severity = "none"
	SeverityLow    Severity = "low"
	SeverityMedium Severity = "medium"
	SeverityHigh   Severity = "high"
)

// Provenance is derived from the current event, never supplied by the model.
// Indirect tool output raises severity but never determines eligibility.
type Provenance struct {
	Indirect bool
}

// SeverityFor starts exclusively from target: guarded is high and unclear is
// medium. Provenance raises and split consensus lowers that base. An eligible
// detection is floored at low, so weighting can never gate it.
func SeverityFor(stabilized Stabilized, provenance Provenance) Severity {
	if !stabilized.IsInjection {
		return SeverityNone
	}

	rank := 1
	switch stabilized.Target {
	case TargetGuardedAgent:
		rank = 3
	case TargetUnclear:
		rank = 2
	}
	if provenance.Indirect {
		rank++
	}
	if !stabilized.Unanimous {
		rank--
	}

	switch {
	case rank >= 3:
		return SeverityHigh
	case rank == 2:
		return SeverityMedium
	default:
		return SeverityLow
	}
}

type Action string

const (
	ActionAllow Action = "allow"
	ActionLog   Action = "would_log"
	ActionWarn  Action = "would_warn"
	ActionBlock Action = "would_block"
)

// Decide computes shadow-only action telemetry. It never changes finding
// publication and no caller in this change automatically enforces ActionBlock.
func Decide(stabilized Stabilized, severity Severity) Action {
	if !stabilized.IsInjection {
		return ActionAllow
	}
	if severity == SeverityHigh && stabilized.Target == TargetGuardedAgent && stabilized.Unanimous {
		return ActionBlock
	}
	if severity == SeverityHigh || severity == SeverityMedium {
		return ActionWarn
	}
	return ActionLog
}
