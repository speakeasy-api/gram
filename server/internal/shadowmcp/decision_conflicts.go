package shadowmcp

import "github.com/google/uuid"

// StandingDecisionConflict names one recorded MCP approval decision that a
// policy URL-list edit would contradict. The edit may proceed only by
// explicitly superseding the decision.
type StandingDecisionConflict struct {
	// RequestID is the approval request whose latest decision conflicts.
	RequestID uuid.UUID
	// TargetKey is the canonical server URL the decision covers.
	TargetKey string
	// TargetRaw is the redacted display form, safe for error messages and
	// audit display names.
	TargetRaw string
	// Decision is the standing decision being contradicted: approved or
	// denied.
	Decision string
}

// StandingDecisionReview is what the policy write path learns about a
// project's standing decisions before touching URL grants.
type StandingDecisionReview struct {
	Conflicts []StandingDecisionConflict
	// StandingURLs is every canonical URL carrying a standing decision. The
	// reconciler leaves these untouched when retained, so a re-sent list
	// never rewrites a decision's recorded audience with the policy's.
	StandingURLs []string
}
