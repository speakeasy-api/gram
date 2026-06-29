package risk_analysis

import "encoding/json"

// AnalyzerConfig is the opaque per-analyzer scanner configuration persisted on
// risk_policies.analyzer_config (JSONB), namespaced by analyzer. New per-scanner
// options live here rather than as a dedicated column each.
type AnalyzerConfig struct {
	Presidio *PresidioConfig `json:"presidio,omitempty"`
}

// PresidioConfig holds presidio-scanner options.
type PresidioConfig struct {
	// ScoreThreshold is the minimum recognizer confidence (0.0-1.0) a match must
	// clear. Absent means "unset" — the scanner applies DefaultPresidioScoreThreshold.
	ScoreThreshold *float64 `json:"score_threshold,omitempty"`
}

// ParseAnalyzerConfig decodes the JSONB blob, returning a zero config for
// nil/empty/invalid input.
func ParseAnalyzerConfig(b []byte) AnalyzerConfig {
	var c AnalyzerConfig
	if len(b) == 0 {
		return c
	}
	_ = json.Unmarshal(b, &c)
	return c
}

// PresidioScoreThresholdFromConfig returns the configured presidio score
// threshold, or 0 when unset. Callers treat 0 as "apply the default"
// (see resolvePresidioScoreThreshold).
func PresidioScoreThresholdFromConfig(b []byte) float64 {
	c := ParseAnalyzerConfig(b)
	if c.Presidio != nil && c.Presidio.ScoreThreshold != nil {
		return *c.Presidio.ScoreThreshold
	}
	return 0
}

// PresidioScoreThresholdPtr returns the configured threshold as *float64 for
// API result mapping, nil when unset.
func PresidioScoreThresholdPtr(b []byte) *float64 {
	c := ParseAnalyzerConfig(b)
	if c.Presidio != nil {
		return c.Presidio.ScoreThreshold
	}
	return nil
}

// WithPresidioScoreThreshold returns analyzer_config JSON with
// presidio.score_threshold set to v (or cleared when v is nil), preserving any
// other analyzer sections already present in base.
func WithPresidioScoreThreshold(base []byte, v *float64) ([]byte, error) {
	c := ParseAnalyzerConfig(base)
	switch {
	case v != nil:
		if c.Presidio == nil {
			c.Presidio = &PresidioConfig{}
		}
		c.Presidio.ScoreThreshold = v
	case c.Presidio != nil:
		c.Presidio.ScoreThreshold = nil
		if (*c.Presidio == PresidioConfig{}) {
			c.Presidio = nil
		}
	}
	return json.Marshal(c)
}
