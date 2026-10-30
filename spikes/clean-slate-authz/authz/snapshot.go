package authz

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type LayerKind uint8

const (
	LayerSubject    LayerKind = iota // the actor's own relationships
	LayerOwner                       // the agent's current owner
	LayerAuthorizer                  // the human who authorized a derived credential
	LayerCeiling                     // a stored credential or session ceiling
)

func (k LayerKind) String() string {
	return [...]string{"subject", "owner", "authorizer", "ceiling"}[k]
}

type anchorKey struct {
	objType, objID, relation string
}

type relKey struct {
	objType, relation string
}

type entry struct {
	id        uuid.UUID
	objID     string
	cond      string
	params    json.RawMessage
	expiresAt time.Time // zero means no expiry
	via       Subject   // which subject in the caller's closure matched
	rel       string    // for Explain
	subject   string    // subject scope, for subject-scoped grants
}

// Snapshot is the caller's grants for one layer: every relationship whose
// subject is the caller or a set the caller belongs to, indexed for lookup.
// Immutable once built and safe to share between requests.
type Snapshot struct {
	Kind       LayerKind
	Subject    Subject
	Revision   int64
	byAnchor   map[anchorKey][]entry
	byRelation map[relKey][]entry
	validUntil time.Time // earliest expiry among entries; zero means never
}

func newSnapshot(kind LayerKind, subject Subject, revision int64, rows []Relationship) *Snapshot {
	s := &Snapshot{Kind: kind, Subject: subject, Revision: revision, byAnchor: map[anchorKey][]entry{}, byRelation: map[relKey][]entry{}}
	for _, r := range rows {
		e := entry{id: r.ID, objID: r.Object.ID, cond: r.ConditionName, params: r.ConditionParams, via: r.Subject, rel: r.String(), subject: r.SubjectScope}
		if r.ExpiresAt != nil {
			e.expiresAt = *r.ExpiresAt
			if s.validUntil.IsZero() || e.expiresAt.Before(s.validUntil) {
				s.validUntil = e.expiresAt
			}
		}
		ak := anchorKey{r.Object.Type, r.Object.ID, r.Relation}
		s.byAnchor[ak] = append(s.byAnchor[ak], e)
		rk := relKey{r.Object.Type, r.Relation}
		s.byRelation[rk] = append(s.byRelation[rk], e)
	}
	return s
}

// Entries returns the snapshot contents for debugging and docs.
func (s *Snapshot) Entries() map[string][]string {
	out := map[string][]string{}
	for k, es := range s.byAnchor {
		key := k.objType + ":" + k.objID + "#" + k.relation
		for _, e := range es {
			out[key] = append(out[key], "via "+e.via.String())
		}
	}
	return out
}

// OrgFacts holds object-side facts about the *target* of a check — which
// groups a person is in, whether they are protected. One per organization and
// revision, shared by every caller.
type OrgFacts struct {
	Revision  int64
	groupsOf  map[string][]string // user id → group ids
	membersOf map[string][]string // group id → user ids
	markers   map[anchorKey]bool  // (type, id, marker)
	marked    map[relKey][]string // (type, marker) → ids
	// Typed memberships ("role:R") for subject-scoped grants.
	typedOf      map[string][]string // user id → ["role:R", ...]
	typedMembers map[string][]string // "role:R" → user ids
}

func NewOrgFacts(revision int64, memberships []Relationship, markers []Relationship) *OrgFacts {
	f := &OrgFacts{Revision: revision, groupsOf: map[string][]string{}, membersOf: map[string][]string{}, markers: map[anchorKey]bool{}, marked: map[relKey][]string{},
		typedOf: map[string][]string{}, typedMembers: map[string][]string{}}
	for _, m := range memberships {
		f.groupsOf[m.Subject.ID] = append(f.groupsOf[m.Subject.ID], m.Object.ID)
		f.membersOf[m.Object.ID] = append(f.membersOf[m.Object.ID], m.Subject.ID)
		typed := m.Object.Type + ":" + m.Object.ID
		f.typedOf[m.Subject.ID] = append(f.typedOf[m.Subject.ID], typed)
		f.typedMembers[typed] = append(f.typedMembers[typed], m.Subject.ID)
	}
	for _, m := range markers {
		f.markers[anchorKey{m.Object.Type, m.Object.ID, m.Relation}] = true
		f.marked[relKey{m.Object.Type, m.Relation}] = append(f.marked[relKey{m.Object.Type, m.Relation}], m.Object.ID)
	}
	return f
}

// Principal is the admitted caller. Every layer must allow every check.
type Principal struct {
	Org          string
	Actor        Subject
	Layers       []*Snapshot
	Facts        *OrgFacts
	Revision     int64
	AdmittedAt   time.Time // database time captured at admission
	admittedMono time.Time
	ValidUntil   time.Time // earliest expiry across layers; zero means never
}

// Now is the authorization clock: database time at admission plus monotonic
// elapsed time, so process clock skew cannot extend access.
func (p *Principal) Now() time.Time {
	return p.AdmittedAt.Add(time.Since(p.admittedMono))
}

// Advance moves the authorization clock forward without waiting. Demo and
// test helper: it simulates a long-lived principal (e.g. a stream) aging.
func (p *Principal) Advance(d time.Duration) { p.AdmittedAt = p.AdmittedAt.Add(d) }

// Expired reports whether the principal must be re-admitted.
func (p *Principal) Expired() bool {
	return !p.ValidUntil.IsZero() && !p.Now().Before(p.ValidUntil)
}
