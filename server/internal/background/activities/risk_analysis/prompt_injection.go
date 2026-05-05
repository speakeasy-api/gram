package risk_analysis

import (
	"context"
	"errors"
	"fmt"
)

// SourcePromptInjection is the policy source value that enables prompt
// injection scanning. Used by both the batch analyzer (writes findings to
// risk_results) and the realtime risk scanner (hook deny path for
// action='block' policies).
const SourcePromptInjection = "prompt_injection"

// classifierFindingRuleID is the rule_id stored on findings produced by
// the model classifier (as opposed to L0 heuristic rules, which carry
// granular pi.* rule IDs).
const classifierFindingRuleID = "pi.classifier-injection"

// ErrClassifierNotConfigured is returned from DetectPromptInjection when a
// policy's UseModelClassifier=true but the runtime is wired with the
// NoopClassifier (env var unset). Misconfiguration is surfaced loudly
// instead of silently degrading to heuristics-only.
var ErrClassifierNotConfigured = errors.New("prompt injection classifier not configured")

// PromptInjectionConfig drives a single scan. Heuristic flags (Detect*)
// default true; the model classifier defaults off. Per-policy JSON config
// overrides these defaults; PromptInjectionConfigDefaults supplies the
// baseline.
type PromptInjectionConfig struct {
	DetectInstructionOverrides bool
	DetectRoleHijack           bool
	DetectSystemPromptLeak     bool
	DetectDelimiterInjection   bool
	DetectEncodedPayloads      bool
	DetectToolAbuse            bool

	HeuristicEmitThreshold float64

	UseModelClassifier      bool
	ModelInjectionThreshold float64
	Classifier              PromptInjectionClassifier
}

// PromptInjectionConfigDefaults returns a config with all heuristic
// families enabled, the classifier off, and conservative thresholds.
func PromptInjectionConfigDefaults() PromptInjectionConfig {
	return PromptInjectionConfig{
		DetectInstructionOverrides: true,
		DetectRoleHijack:           true,
		DetectSystemPromptLeak:     true,
		DetectDelimiterInjection:   true,
		DetectEncodedPayloads:      true,
		DetectToolAbuse:            true,
		HeuristicEmitThreshold:     0.6,
		UseModelClassifier:         false,
		ModelInjectionThreshold:    0.85,
		Classifier:                 nil,
	}
}

// DetectPromptInjection scans text for prompt-injection signals using L0
// heuristic rules and, when UseModelClassifier is set, an L1 model
// classifier. Returns one Finding per match. Returns
// ErrClassifierNotConfigured when the policy enables the classifier but
// the runtime has no real backend wired in.
func DetectPromptInjection(ctx context.Context, text string, cfg PromptInjectionConfig) ([]Finding, error) {
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

	if !cfg.UseModelClassifier {
		return out, nil
	}

	if cfg.Classifier == nil || IsNoopClassifier(cfg.Classifier) {
		return nil, ErrClassifierNotConfigured
	}

	verdicts, err := cfg.Classifier.Classify(ctx, []string{text})
	if err != nil {
		return nil, fmt.Errorf("prompt injection classifier: %w", err)
	}
	if len(verdicts) != 1 {
		return nil, fmt.Errorf("prompt injection classifier returned %d verdicts for 1 text", len(verdicts))
	}

	v := verdicts[0]
	if v.Injection && v.Score >= cfg.ModelInjectionThreshold {
		out = append(out, Finding{
			RuleID:      classifierFindingRuleID,
			Description: "Model classifier flagged input as prompt injection",
			Match:       "",
			StartPos:    0,
			EndPos:      len(text),
			Source:      SourcePromptInjection,
			Confidence:  v.Score,
			Tags:        nil,
		})
	}

	return out, nil
}
