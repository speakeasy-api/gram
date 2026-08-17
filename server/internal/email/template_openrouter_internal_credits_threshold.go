package email

import "strconv"

// OpenRouterInternalCreditsThreshold is the stable template contract for
// Security inference alerts. The underlying provider key still uses the
// `internal` storage identifier. The alert is sent at 50%, 75%, 90%, and 100%
// of that key's monthly cap.
type OpenRouterInternalCreditsThreshold struct {
	// OrganizationName is the display name of the organization.
	OrganizationName string
	// ThresholdPercent is the crossed threshold, e.g. "90".
	ThresholdPercent string
	// Exhausted reports whether the cap is fully used (the 100% threshold).
	// Loops branches its copy on this so a single template covers both the
	// approach warnings and the hard exhaustion notice.
	Exhausted bool
}

func (t OpenRouterInternalCreditsThreshold) Key() TemplateKey {
	return TemplateKeyOpenRouterInternalCredits
}

func (t OpenRouterInternalCreditsThreshold) AddToAudience() bool { return false }

func (t OpenRouterInternalCreditsThreshold) Variables() map[string]string {
	return map[string]string{
		"organization_name": t.OrganizationName,
		"threshold_percent": t.ThresholdPercent,
		"exhausted":         strconv.FormatBool(t.Exhausted),
	}
}
