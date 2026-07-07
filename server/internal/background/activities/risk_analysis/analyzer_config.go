package risk_analysis

import (
	"encoding/json"
	"fmt"
)

// AnalyzerConfig is the opaque per-analyzer scanner configuration persisted on
// risk_policies.analyzer_config (JSONB), namespaced by analyzer. New per-scanner
// options live here rather than as a dedicated column each.
type AnalyzerConfig struct {
	Presidio        *PresidioConfig        `json:"presidio,omitempty"`
	AccountIdentity *AccountIdentityConfig `json:"account_identity,omitempty"`
}

// PresidioConfig holds presidio-scanner options.
type PresidioConfig struct {
	// ScoreThreshold is the minimum recognizer confidence (0.0-1.0) a match must
	// clear. Absent means "unset" — the scanner applies DefaultPresidioScoreThreshold.
	ScoreThreshold *float64 `json:"score_threshold,omitempty"`
}

// AccountIdentityConfig holds account_identity-scanner options.
type AccountIdentityConfig struct {
	// ApprovedEmailDomains is the corporate email domain allowlist. When
	// non-empty, sessions whose AI-account email domain is not in the list
	// produce an identity.unapproved_domain finding. Empty means the domain
	// rule is inert.
	ApprovedEmailDomains []string `json:"approved_email_domains,omitempty"`
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

// ApprovedEmailDomainsFromConfig returns the configured corporate email
// domain allowlist, or nil when unset.
func ApprovedEmailDomainsFromConfig(b []byte) []string {
	c := ParseAnalyzerConfig(b)
	if c.AccountIdentity != nil {
		return c.AccountIdentity.ApprovedEmailDomains
	}
	return nil
}

// WithApprovedEmailDomains returns analyzer_config JSON with
// account_identity.approved_email_domains set to v, or cleared when v is
// empty. Only fields known to AnalyzerConfig are round-tripped; any
// unrecognized keys in base are dropped.
func WithApprovedEmailDomains(base []byte, v []string) ([]byte, error) {
	c := ParseAnalyzerConfig(base)
	switch {
	case len(v) > 0:
		c.AccountIdentity = &AccountIdentityConfig{ApprovedEmailDomains: v}
	case c.AccountIdentity != nil:
		// ApprovedEmailDomains is account_identity's only field today; clearing
		// it leaves the section empty, so drop it entirely.
		c.AccountIdentity = nil
	}
	out, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("marshal analyzer config: %w", err)
	}
	return out, nil
}

// WithPresidioScoreThreshold returns analyzer_config JSON with
// presidio.score_threshold set to v, or cleared when v is nil. Only fields known
// to AnalyzerConfig are round-tripped; any unrecognized keys in base are dropped.
func WithPresidioScoreThreshold(base []byte, v *float64) ([]byte, error) {
	c := ParseAnalyzerConfig(base)
	switch {
	case v != nil:
		if c.Presidio == nil {
			c.Presidio = &PresidioConfig{ScoreThreshold: v}
		} else {
			c.Presidio.ScoreThreshold = v
		}
	case c.Presidio != nil:
		// ScoreThreshold is presidio's only field today; clearing it leaves the
		// section empty, so drop it entirely.
		c.Presidio = nil
	}
	out, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("marshal analyzer config: %w", err)
	}
	return out, nil
}
