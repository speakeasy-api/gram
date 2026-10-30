package xrbac

import (
	"slices"
	"time"
)

// AuthMethod records how the subject authenticated.
type AuthMethod string

const (
	AuthSession  AuthMethod = "session"
	AuthAgentKey AuthMethod = "agent_key"
)

// Subject is identity only — never grants. Comes from trusted authentication.
type Subject struct {
	Org             string
	Actor           string // "user:alice", "agent:agt_triage"
	Method          AuthMethod
	CredentialID    string // the API key for AuthAgentKey
	RepresentedUser string // empty unless the credential contract binds one
}

// Ref is one resource reference.
type Ref struct {
	Org  string
	Kind string // "org", "project", "mcp", "agent", "chat"
	ID   string
}

// Target is a concrete resource plus its actual containing resources, loaded
// by the handler from tenant-scoped records.
type Target struct {
	Resource  Ref
	Ancestors []Ref // nearest first
}

func (t Target) ancestor(kind string) (Ref, bool) {
	for _, a := range t.Ancestors {
		if a.Kind == kind {
			return a, true
		}
	}
	return Ref{}, false
}

type QuestionKind uint8

const (
	ExistingResource QuestionKind = iota
	ToolCall
)

const ClassificationUnknown = "unknown"

// ToolFacts describe a selected tool. Unknown classification stays explicit.
type ToolFacts struct {
	Name           string
	Classification string // "read_only", "destructive", ... or ClassificationUnknown
}

type Request struct {
	Scope  Scope
	Target Target
	Kind   QuestionKind
	Tool   *ToolFacts
}

func ResourceCheck(s Scope, t Target) Request {
	return Request{Scope: s, Target: t, Kind: ExistingResource}
}

func ToolCallCheck(s Scope, t Target, f ToolFacts) Request {
	return Request{Scope: s, Target: t, Kind: ToolCall, Tool: &f}
}

// Effect is explicit internally. Legacy blocked scopes decode to EffectDeny.
type Effect uint8

const (
	EffectAllow Effect = iota
	EffectDeny
)

// Rule is the normalized form of a stored grant (or a relationship rule).
type Rule struct {
	ID         string
	Scope      Scope // the scope allowed or denied
	Effect     Effect
	Selector   map[string]string
	ValidFrom  *time.Time
	ValidUntil *time.Time // end-exclusive
	Source     string     // "user:alice", "role:role_eng", "relationship:owner", "credential:key_123"
}

func (r Rule) validAt(now time.Time) bool {
	return (r.ValidFrom == nil || !now.Before(*r.ValidFrom)) && (r.ValidUntil == nil || now.Before(*r.ValidUntil))
}

type LimitKind string

const (
	LimitSubject    LimitKind = "subject"
	LimitCredential LimitKind = "credential"
	LimitAgent      LimitKind = "agent"
	LimitOwner      LimitKind = "owner"
)

// PolicyLimit is one separately required policy. An empty one refuses.
type PolicyLimit struct {
	Kind  LimitKind
	Rules []Rule
}

type Outcome uint8

const (
	Denied Outcome = iota
	Allowed
	Unresolved
)

func (o Outcome) String() string { return [...]string{"denied", "allowed", "unresolved"}[o] }

type Reason string

const (
	ReasonAllowed       Reason = "allowed"
	ReasonNoGrant       Reason = "no_matching_grant"
	ReasonDenied        Reason = "matching_denial"
	ReasonFactsMissing  Reason = "facts_unavailable"
	ReasonLimitFailed   Reason = "limit_failed"
	ReasonExceedsParent Reason = "exceeds_parent_authority"
	ReasonNotDelegable  Reason = "not_delegable"
)

type Decision struct {
	Outcome Outcome
	Reason  Reason
	Limit   LimitKind // which limit decided, when not Allowed
	Source  string    // disclosure-safe provenance of the deciding rule
}

func (d Decision) RequireAllowed() error {
	if d.Outcome == Allowed {
		return nil
	}
	return &DecisionError{d}
}

type DecisionError struct{ D Decision }

func (e *DecisionError) Error() string {
	return "forbidden: " + string(e.D.Reason) + " (" + e.D.Outcome.String() + ", limit " + string(e.D.Limit) + ")"
}

// AccessQuery asks whether any tool call on a server is permitted.
type AccessQuery struct {
	Scope  Scope
	Target Target
}

// Delegation is the complete policy proposed for an agent.
type Delegation struct {
	Agent    Target
	Proposed []Rule
}

type Explanation struct {
	Decision Decision
	Detail   []string
}

// Evaluation is the validated question handed to the backend.
type Evaluation struct {
	Subject    Subject
	Request    Request
	Definition ScopeDefinition
}

func cloneRequest(r Request) Request {
	r.Target.Ancestors = slices.Clone(r.Target.Ancestors)
	if r.Tool != nil {
		t := *r.Tool
		r.Tool = &t
	}
	return r
}
