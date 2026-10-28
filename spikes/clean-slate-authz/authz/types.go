// Package authz is the evaluator: admission builds a Principal of per-layer
// snapshots, and Check/Lookup evaluate compiled schema expressions against it
// with no I/O.
package authz

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Subject is a principal (user:alice) or a subject set (role:role_eng#assignee).
type Subject struct {
	Type     string
	ID       string
	Relation string
}

func (s Subject) String() string {
	if s.Relation == "" {
		return s.Type + ":" + s.ID
	}
	return s.Type + ":" + s.ID + "#" + s.Relation
}

func User(id string) Subject   { return Subject{Type: "user", ID: id} }
func Agent(id string) Subject  { return Subject{Type: "agent", ID: id} }
func APIKey(id string) Subject { return Subject{Type: "api_key", ID: id} }

// ObjectID names a stored object.
type ObjectID struct {
	Type string
	ID   string
}

func (o ObjectID) String() string { return o.Type + ":" + o.ID }

// Relationship is one row of authz_relationships.
type Relationship struct {
	ID              uuid.UUID
	Object          ObjectID
	Relation        string
	Subject         Subject
	ConditionName   string
	ConditionParams json.RawMessage
	ExpiresAt       *time.Time
	// SubjectScope limits a subject-scoped grant: "*", "user:X", "agent:A" or
	// "role:R" (members of R). Empty for ordinary grants.
	SubjectScope string
	Source       string
	SourceRef    string
}

func (r Relationship) String() string {
	return fmt.Sprintf("%s#%s@%s", r.Object, r.Relation, r.Subject)
}

// Ref is a protected object with the facts needed to evaluate it without I/O.
// Handlers obtain one from a generated objects.* struct, never by hand.
type Ref struct {
	Type    string
	ID      string
	Parents []Ref            // nearest first; the organization is last
	Attrs   map[string][]Ref // owner, author, agent: read from the loaded row
}

func (r Ref) Org() string {
	if r.Type == "organization" {
		return r.ID
	}
	if n := len(r.Parents); n > 0 {
		return r.Parents[n-1].ID
	}
	return ""
}

func (r Ref) parent(typ string) (Ref, bool) {
	for _, p := range r.Parents {
		if p.Type == typ {
			return p, true
		}
	}
	return Ref{}, false
}

// Object is implemented by every generated objects.* struct.
type Object interface{ AuthzRef() Ref }

// Permission is bound to its object type, so a server permission cannot be
// checked against a project: perm.MCPServerEdit is Permission[objects.MCPServer].
type Permission[T Object] struct {
	Type string
	Name string
}

func (p Permission[T]) String() string { return p.Type + "." + p.Name }

type Outcome uint8

const (
	Denied Outcome = iota
	Allowed
	Undetermined // a needed fact was missing; treated as denied, with diagnostics
)

func (o Outcome) String() string {
	return [...]string{"denied", "allowed", "undetermined"}[o]
}

type Decision struct {
	Outcome Outcome
	Perm    string
	Layer   LayerKind // the first layer that refused, when not Allowed
	Reason  string
	Path    []string // relationships that produced an allow (Explain)
}

func (d Decision) Allowed() bool { return d.Outcome == Allowed }

// Credential is what authentication produced. Authorization starts here.
type Credential struct {
	Org    string
	UserID string // set for a human session
	KeyID  string // set for an agent API key
}
