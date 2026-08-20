package shadowmcp

import "github.com/google/uuid"

// StandingDecisionConflict names one recorded MCP approval decision that a
// policy URL-list edit would contradict: removing an approved server's allow,
// allow-listing a denied server, block-listing an approved one, or removing a
// denied server's block. The edit may proceed only by explicitly superseding
// the decision, so a decision record can never be contradicted silently.
type StandingDecisionConflict struct {
	// RequestID is the approval request whose latest decision conflicts.
	RequestID uuid.UUID
	// TargetKey is the canonical server URL the decision covers.
	TargetKey string
	// TargetRaw is the redacted display form of the server reference, safe
	// for error messages and audit display names.
	TargetRaw string
	// Decision is the standing decision being contradicted: approved or
	// denied.
	Decision string
}

// StandingDecisionReview is what the policy write path learns about a
// project's standing decisions before touching URL grants.
type StandingDecisionReview struct {
	Conflicts []StandingDecisionConflict
	// StandingURLs is every canonical server URL carrying a standing
	// (non-superseded) decision, conflicted or not. The reconciler leaves
	// these untouched when they are retained: their grants carry the
	// decision's recorded blast radius, and a policy save that merely
	// re-sends the list must not rewrite a decision's audience with the
	// policy's.
	StandingURLs []string
}
