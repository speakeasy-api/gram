package email

import "strconv"

// OpenRouterChatCreditsThreshold is sent to an organization's billing alert
// contact when usage of the platform-managed OpenRouter chat key crosses a
// warning threshold (50%, 75%, 90%) or exhausts (100%) the monthly credit cap.
// Exhausting the chat cap causes the org's chat surfaces (playground,
// elements, assistants, /chat/completions proxy) to start returning 402/5XX,
// so the warnings give admins a chance to react before that happens.
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

func (t OpenRouterChatCreditsThreshold) TransactionalID() TransactionalID {
	return transactionalIDOpenRouterChatCreditsThreshold
}

func (t OpenRouterChatCreditsThreshold) AddToAudience() bool { return false }

func (t OpenRouterChatCreditsThreshold) Variables() map[string]string {
	return map[string]string{
		"organization_name": t.OrganizationName,
		"threshold_percent": t.ThresholdPercent,
		"exhausted":         strconv.FormatBool(t.Exhausted),
	}
}
