package authz

import (
	"context"
	"errors"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/authz/schema"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Facts are the verified resource attributes a service loads before asking
// for a permission that reads them. The evaluator never loads facts itself:
// services own lookups, transactions and locks, and hand the results over.
//
// A fact is always in exactly one state, and the states are not
// interchangeable:
//
//   - present: loaded with a value. An empty string is a value.
//   - absent: the lookup completed and established there is no value, such
//     as a NULL column or no qualifying related record. Absence never
//     matches an identity or relationship.
//   - unloaded: nobody loaded it. This is the zero Fact.
//   - invalid: loaded, but the record contradicts what was claimed about it,
//     such as a related record in another tenant.
//   - load failed: the lookup itself failed or was cancelled.
//
// Only present and absent facts resolve. A permission that needs an
// unresolved fact to decide errors; it never treats the fact as absent.

// FactState is the state of one loaded fact.
type FactState uint8

const (
	// FactUnloaded is the zero state: nothing was loaded.
	FactUnloaded FactState = iota
	// FactPresent holds a value.
	FactPresent
	// FactAbsent is known absence, such as NULL or no qualifying record.
	FactAbsent
	// FactInvalid is a loaded value that contradicts its claimed context.
	FactInvalid
	// FactLoadFailed is a lookup that failed or was cancelled.
	FactLoadFailed
)

func (s FactState) String() string {
	switch s {
	case FactUnloaded:
		return "unloaded"
	case FactPresent:
		return "present"
	case FactAbsent:
		return "absent"
	case FactInvalid:
		return "invalid"
	case FactLoadFailed:
		return "load_failed"
	default:
		return fmt.Sprintf("fact_state(%d)", uint8(s))
	}
}

// Fact is one resource attribute as a service loaded it. Build it with the
// constructors; the zero Fact is unloaded.
type Fact struct {
	state   FactState
	kind    factKind
	text    string
	boolean bool
	related *RelatedResource
	err     error
}

// factKind records which constructor built a present fact, so a value of one
// kind can never stand in for another, such as a bare PresentFact read as a
// false bool.
type factKind uint8

const (
	factKindNone factKind = iota
	factKindText
	factKindBool
	factKindPresence
	factKindRelated
)

// RelatedResource is the target of a relationship fact, loaded one hop away
// with the facts its own permissions read.
type RelatedResource struct {
	// ID is the related resource's concrete ID.
	ID string

	// Dimensions narrow grant matching on the related resource, as
	// PermissionRequest.Dimensions does.
	Dimensions map[string]string

	// Facts are the related resource's own loaded facts. Traversal is one hop,
	// so they may not hold further relationships.
	Facts map[string]Fact
}

// StringFact is a present string or user fact. An empty string is present.
func StringFact(value string) Fact {
	return Fact{state: FactPresent, kind: factKindText, text: value, boolean: false, related: nil, err: nil}
}

// BoolFact is a present bool fact.
func BoolFact(value bool) Fact {
	return Fact{state: FactPresent, kind: factKindBool, text: "", boolean: value, related: nil, err: nil}
}

// PresentFact is a present fact with no comparable value, such as a non-NULL
// timestamp.
func PresentFact() Fact {
	return Fact{state: FactPresent, kind: factKindPresence, text: "", boolean: false, related: nil, err: nil}
}

// RelatedFact is a present relationship to a verified related resource.
func RelatedFact(resource RelatedResource) Fact {
	return Fact{state: FactPresent, kind: factKindRelated, text: "", boolean: false, related: &resource, err: nil}
}

// AbsentFact is known absence: a NULL value, or a completed lookup that found
// no qualifying related record.
func AbsentFact() Fact {
	return Fact{state: FactAbsent, kind: factKindNone, text: "", boolean: false, related: nil, err: nil}
}

// InvalidFact is a loaded value that contradicts its claimed context.
func InvalidFact(err error) Fact {
	return Fact{state: FactInvalid, kind: factKindNone, text: "", boolean: false, related: nil, err: err}
}

// LoadFailedFact is a lookup that failed or was cancelled.
func LoadFailedFact(err error) Fact {
	return Fact{state: FactLoadFailed, kind: factKindNone, text: "", boolean: false, related: nil, err: err}
}

// State returns the fact's state.
func (f Fact) State() FactState { return f.state }

// ErrFactUnresolved reports a permission that needed a fact that was
// unloaded, invalid or failed to load.
var ErrFactUnresolved = errors.New("authorization fact is unresolved")

func (f Fact) unresolved(name string) error {
	if f.err != nil {
		return fmt.Errorf("%w: %s is %s: %w", ErrFactUnresolved, name, f.state, f.err)
	}
	return fmt.Errorf("%w: %s is %s", ErrFactUnresolved, name, f.state)
}

// kindFor is the kind a present fact of the declared fact type must have.
// A declared type name is a relationship.
func kindFor(factType string) factKind {
	switch factType {
	case schema.FactString, schema.FactNullableString, schema.FactUser:
		return factKindText
	case schema.FactBool:
		return factKindBool
	case schema.FactNullableTimestamp:
		return factKindPresence
	default:
		return factKindRelated
	}
}

func (k factKind) String() string {
	switch k {
	case factKindNone:
		return "none"
	case factKindText:
		return "string"
	case factKindBool:
		return "bool"
	case factKindPresence:
		return "presence"
	case factKindRelated:
		return "relationship"
	default:
		return fmt.Sprintf("fact_kind(%d)", uint8(k))
	}
}

func (f Fact) resolved() bool {
	return f.state == FactPresent || f.state == FactAbsent
}

// RelationshipLoader resolves one relationship hop for a service before it
// evaluates a permission that traverses it, such as a chat's agent.
//
// The loader runs inside the caller's transaction and organization. Its
// result must be:
//
//   - RelatedFact with the target's facts when the target exists in
//     organizationID and qualifies;
//   - AbsentFact when the lookup completed and found no qualifying target,
//     including a target ID that names nothing in this organization, without
//     probing any other tenant;
//   - InvalidFact when a supplied record contradicts organizationID;
//   - LoadFailedFact when the lookup failed or ctx was cancelled.
//
// A stored target ID alone proves nothing, so callers never build a
// RelatedFact without loading the target.
type RelationshipLoader interface {
	LoadRelated(ctx context.Context, organizationID, targetType, targetID string) Fact
}

// truth is the three-valued result of a guard or expression: it matched, it
// did not, or it could not be decided.
type truth uint8

const (
	truthFalse truth = iota
	truthTrue
	truthUnknown
)

// guardResult evaluates a guard condition over facts. All is false when any
// operand is false, so a false guard skips dependent facts whatever their
// order; otherwise any unresolved operand leaves it unknown.
func guardResult(cond schema.Cond, facts map[string]Fact) (truth, error) {
	if cond.Op() == schema.CondAll {
		result := truthTrue
		var firstErr error
		for _, operand := range cond.Operands() {
			t, err := guardResult(operand, facts)
			switch t {
			case truthFalse:
				return truthFalse, nil
			case truthUnknown:
				result = truthUnknown
				if firstErr == nil {
					firstErr = err
				}
			case truthTrue:
			}
		}
		return result, firstErr
	}

	name := cond.Fact()
	fact := facts[name]
	if !fact.resolved() {
		return truthUnknown, fact.unresolved(name)
	}
	present := fact.state == FactPresent
	switch cond.Op() {
	case schema.CondIsNull:
		return boolTruth(!present), nil
	case schema.CondPresent:
		return boolTruth(present), nil
	case schema.CondNullOrEmpty:
		return boolTruth(!present || fact.text == ""), nil
	case schema.CondIsFalse:
		if !present {
			return truthUnknown, fmt.Errorf("%w: bool fact %s is absent", ErrFactUnresolved, name)
		}
		return boolTruth(!fact.boolean), nil
	case schema.CondAll:
		return truthUnknown, errors.New("unreachable condition")
	default:
		return truthUnknown, fmt.Errorf("%w: unknown condition %q", ErrUnsupportedExpression, cond.Op())
	}
}

func boolTruth(b bool) truth {
	if b {
		return truthTrue
	}
	return truthFalse
}

// actorIs reports whether actor is the user principal a user fact holds. An
// absent fact or an empty user ID names nobody, so it never matches.
func actorIs(actor urn.Principal, fact Fact) bool {
	if fact.state != FactPresent || fact.text == "" {
		return false
	}
	return actor.Type == urn.PrincipalTypeUser && actor.ID != "" && actor.ID == fact.text
}
