package email

import "strconv"

// OpenRouterChatCreditsThreshold is the stable template contract for Other
// inference alerts. The underlying provider key still uses the legacy `chat`
// storage identifier, but customers see the function it serves. The alert is
// sent at 50%, 75%, 90%, and 100% of that key's monthly cap.
type OpenRouterChatCreditsThreshold struct {
	// OrganizationName is the display name of the organization.
	OrganizationName string
	// ThresholdPercent is the crossed threshold, e.g. "90".
	ThresholdPercent string
	// Exhausted reports whether the cap is fully used (the 100% threshold).
	// Loops branches its copy on this so a single template covers both the
	// approach warnings and the hard exhaustion notice.
	Exhausted bool
}

func (t OpenRouterChatCreditsThreshold) Key() TemplateKey {
	return TemplateKeyOpenRouterChatCredits
}

func (t OpenRouterChatCreditsThreshold) AddToAudience() bool { return false }

func (t OpenRouterChatCreditsThreshold) Variables() map[string]string {
	return map[string]string{
		"organization_name": t.OrganizationName,
		"threshold_percent": t.ThresholdPercent,
		"exhausted":         strconv.FormatBool(t.Exhausted),
	}
}
