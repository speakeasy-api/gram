package repo

import "time"

// Operation identifies which authz API was invoked.
type Operation string

const (
	OperationRequire    Operation = "require"
	OperationRequireAny Operation = "require_any"
	OperationFilter     Operation = "filter"
)

// Outcome is the decision recorded for the challenge.
type Outcome string

const (
	OutcomeAllow Outcome = "allow"
	OutcomeDeny  Outcome = "deny"
	OutcomeError Outcome = "error"
)

// Reason narrates why the outcome was reached. Kept LowCardinality on the CH
// side; treat as an open enum on the Go side.
type Reason string

const (
	ReasonGrantMatched      Reason = "grant_matched"
	ReasonNoGrants          Reason = "no_grants"
	ReasonScopeUnsatisfied  Reason = "scope_unsatisfied"
	ReasonInvalidCheck      Reason = "invalid_check"
	ReasonRBACSkippedAPIKey Reason = "rbac_skipped_apikey" //nolint:gosec // not a credential
	ReasonDevOverride       Reason = "dev_override"
)

// PrincipalType is the kind of caller making the check.
type PrincipalType string

const (
	PrincipalTypeUser      PrincipalType = "user"
	PrincipalTypeAPIKey    PrincipalType = "api_key"
	PrincipalTypeAssistant PrincipalType = "assistant"
)

// ChallengeRow mirrors one row of the authz_challenges table.
type ChallengeRow struct {
	ID             string
	Timestamp      time.Time
	OrganizationID string
	ProjectID      string
	TraceID        string
	SpanID         string
	RequestID      *string

	PrincipalURN   string
	PrincipalType  PrincipalType
	UserID         *string
	UserExternalID *string
	UserEmail      *string
	APIKeyID       *string
	SessionID      *string
	RoleSlugs      []string

	Operation Operation
	Outcome   Outcome
	Reason    Reason

	Scope          string
	ResourceKind   string
	ResourceID     string
	Selector       string
	ExpandedScopes []string

	RequestedChecks []RequestedCheck
	MatchedGrants   []MatchedGrant

	EvaluatedGrantCount uint32

	FilterCandidateCount uint32
	FilterAllowedCount   uint32
}

// RequestedCheck mirrors one element of the requested_checks Nested column.
type RequestedCheck struct {
	Scope        string
	ResourceKind string
	ResourceID   string
	Selector     string
}

// MatchedGrant mirrors one element of the matched_grants Nested column.
type MatchedGrant struct {
	PrincipalURN         string
	Scope                string
	Selector             string
	MatchedViaCheckScope string
}
