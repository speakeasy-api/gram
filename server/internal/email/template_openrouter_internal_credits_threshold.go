package email

import "strconv"

// OpenRouterInternalCreditsThreshold is sent to an organization's billing
// alert contact when usage of the platform-managed OpenRouter internal key —
// the one paying for platform-initiated analysis (risk-policy judges, prompt
// injection detection, chat titles, resolution analysis, memory) — crosses a
// warning threshold (50%, 75%, 90%) or exhausts (100%) the monthly credit cap.
// Exhausting the internal cap silently degrades that analysis coverage, so
// the warnings give admins a chance to react before it lapses.
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

func (t OpenRouterInternalCreditsThreshold) TransactionalID() TransactionalID {
	return transactionalIDOpenRouterInternalCreditsThreshold
}

func (t OpenRouterInternalCreditsThreshold) AddToAudience() bool { return false }

func (t OpenRouterInternalCreditsThreshold) Variables() map[string]string {
	return map[string]string{
		"organization_name": t.OrganizationName,
		"threshold_percent": t.ThresholdPercent,
		"exhausted":         strconv.FormatBool(t.Exhausted),
	}
}
