package email

// OpenRouterCreditsThreshold is sent to an organization's admins when usage of
// the platform-managed OpenRouter chat key crosses a warning threshold (50%,
// 75%, 90%) or exhausts (100%) the monthly credit cap. Exhausting the cap
// causes the org's chat surfaces (playground, elements, assistants,
// /chat/completions proxy) to start returning 402/5XX, so the warnings give
// admins a chance to react before that happens.
type OpenRouterCreditsThreshold struct {
	// OrganizationName is the display name of the organization.
	OrganizationName string
	// ThresholdPercent is the crossed threshold, e.g. "90".
	ThresholdPercent string
	// Exhausted reports whether the cap is fully used (the 100% threshold).
	// Loops branches its copy on this so a single template covers both the
	// approach warnings and the hard exhaustion notice.
	Exhausted bool
}

func (t OpenRouterCreditsThreshold) TransactionalID() TransactionalID {
	return transactionalIDOpenRouterCreditsThreshold
}

func (t OpenRouterCreditsThreshold) AddToAudience() bool { return false }

func (t OpenRouterCreditsThreshold) Variables() map[string]string {
	exhausted := "false"
	if t.Exhausted {
		exhausted = "true"
	}
	return map[string]string{
		"organization_name": t.OrganizationName,
		"threshold_percent": t.ThresholdPercent,
		"exhausted":         exhausted,
	}
}
