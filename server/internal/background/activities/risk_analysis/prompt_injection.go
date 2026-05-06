package risk_analysis

import (
	"context"
)

// SourcePromptInjection is the policy source value that enables prompt
// injection scanning. Used by both the batch analyzer (writes findings to
// risk_results) and the realtime risk scanner (hook deny path for
// action='block' policies).
const SourcePromptInjection = "prompt_injection"

// PromptInjectionConfig drives a single scan. Heuristic flags (Detect*)
// default true. Per-policy JSON config overrides these defaults;
// PromptInjectionConfigDefaults supplies the baseline.
type PromptInjectionConfig struct {
	DetectInstructionOverrides bool
	DetectRoleHijack           bool
	DetectSystemPromptLeak     bool
	DetectDelimiterInjection   bool
	DetectEncodedPayloads      bool
	DetectToolAbuse            bool

	HeuristicEmitThreshold float64
}

// PromptInjectionConfigDefaults returns a config with all heuristic
// families enabled and a conservative emit threshold.
func PromptInjectionConfigDefaults() PromptInjectionConfig {
	return PromptInjectionConfig{
		DetectInstructionOverrides: true,
		DetectRoleHijack:           true,
		DetectSystemPromptLeak:     true,
		DetectDelimiterInjection:   true,
		DetectEncodedPayloads:      true,
		DetectToolAbuse:            true,
		HeuristicEmitThreshold:     0.6,
	}
}

// DetectPromptInjection scans text for prompt-injection signals using L0
// heuristic rules. Returns one Finding per match.
func DetectPromptInjection(_ context.Context, text string, cfg PromptInjectionConfig) ([]Finding, error) {
	if text == "" {
		return nil, nil
	}

	findings := runHeuristics(text, cfg)
	out := make([]Finding, 0, len(findings))
	for _, f := range findings {
		if f.Confidence < cfg.HeuristicEmitThreshold {
			continue
		}
		out = append(out, f)
	}

	return out, nil
}
