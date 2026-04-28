package audit

// LogURNNamingBadEvent has both an exempt scope id and a flagged subject id.
type LogURNNamingBadEvent struct {
	OrganizationID string
	ProjectID      string
	SubjectID      string // want "use URN field naming and URN type instead of ID"
	OtherId        string // want "use URN field naming and URN type instead of ID"
	OtherName      string
}

// LogURNNamingGoodEvent uses URN-suffixed names exclusively (modulo the
// exempt scope ids), so the rule must not fire.
type LogURNNamingGoodEvent struct {
	OrganizationID string
	ProjectID      string
	SubjectURN     string
	OtherUrn       string
}

// LogURNNamingNamesContainingIdEvent exercises the suffix match: Identifier
// contains the substring "Id" but does not end in Id/ID and must not be
// flagged.
type LogURNNamingNamesContainingIdEvent struct {
	OrganizationID string
	ProjectID      string
	Identifier     string
	Hidden         string
}

// LogURNNamingNonExemptScopeEvent confirms that fields like SourceProjectID
// are not covered by the ProjectID exemption.
type LogURNNamingNonExemptScopeEvent struct {
	OrganizationID  string
	ProjectID       string
	SourceProjectID string // want "use URN field naming and URN type instead of ID"
}

// LogURNNamingTypedSubjectEvent confirms the rule fires regardless of the
// subject id field's Go type — naming alone is enough.
type LogURNNamingTypedSubjectEvent struct {
	OrganizationID string
	ProjectID      string
	SubjectID      [16]byte // want "use URN field naming and URN type instead of ID"
}
