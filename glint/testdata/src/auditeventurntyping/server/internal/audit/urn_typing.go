package audit

import "auditeventurntyping/server/internal/urn"

// LogURNTypingBadEvent declares URN-named fields with non-urn types and
// must be flagged on each.
type LogURNTypingBadEvent struct {
	OrganizationID string
	ProjectID      string
	SubjectURN     string // want "use server/internal/urn type"
	OtherUrn       int    // want "use server/internal/urn type"
}

// LogURNTypingGoodEvent declares URN-named fields with urn-package types
// (direct and pointer) and must not be flagged.
type LogURNTypingGoodEvent struct {
	OrganizationID string
	ProjectID      string
	ActorURN       urn.Principal
	SubjectURN     *urn.RemoteMcpServer
}

// LogURNTypingNoURNFieldsEvent has no URN-named fields and must not be
// flagged regardless of its other field types.
type LogURNTypingNoURNFieldsEvent struct {
	OrganizationID string
	ProjectID      string
	SubjectID      string
}
