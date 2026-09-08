package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"net/mail"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// PrincipalType represents the type prefix of a principal URN.
type PrincipalType string

const (
	PrincipalTypeUser  PrincipalType = "user"
	PrincipalTypeRole  PrincipalType = "role"
	PrincipalTypeEmail PrincipalType = "email"
	PrincipalTypeAgent PrincipalType = "agent"
	// PrincipalTypeSystem is a Gram component acting with no request behind it; new background work audits as it, older writers still audit as "user:system".
	PrincipalTypeSystem PrincipalType = "system"
)

// PrincipalWildcard is the URN that matches any principal in the org. It is
// not a typed `type:id` Principal — assignment tables store this literal
// alongside the typed URNs, so consumers special-case it.
const PrincipalWildcard = "*"

// AllUsersPrincipalID is reserved for the user:all subject-set principal.
// It must not be resolved as a concrete Gram user ID.
const AllUsersPrincipalID = "all"

var principalTypes = map[PrincipalType]struct{}{
	PrincipalTypeUser:   {},
	PrincipalTypeRole:   {},
	PrincipalTypeEmail:  {},
	PrincipalTypeAgent:  {},
	PrincipalTypeSystem: {},
}

// systemPrincipalIDPattern is the lowercase kebab-case component name a system principal carries.
var systemPrincipalIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// Principal is a 2-segment URN that identifies a principal in the RBAC system.
// Format: "type:id" where type is "user", "role", "email", "agent", or
// "system" and id is the principal identifier (e.g. "user:user_01abc",
// "user:all", "role:admin", "email:dev@example.com", "agent:<uuid>", or
// "system:issuer-metadata-refresh").
type Principal struct {
	Type PrincipalType
	ID   string

	checked bool
	err     error
}

// NewPrincipal creates a new Principal URN. Validation runs eagerly;
// call MarshalJSON / Value to surface any cached error.
func NewPrincipal(typ PrincipalType, id string) Principal {
	p := Principal{
		Type:    typ,
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = p.validate()

	return p
}

// NewSystemPrincipal names a Gram component acting on its own, for audit entries background work writes.
func NewSystemPrincipal(component string) Principal {
	return NewPrincipal(PrincipalTypeSystem, component)
}

// ParsePrincipal parses a string of the form "type:id" into a Principal.
func ParsePrincipal(value string) (Principal, error) {
	if value == "" {
		return Principal{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	// Split into exactly 2 segments on the first delimiter only, so that the
	// ID segment can itself contain colons (e.g. future composite IDs).
	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" {
		return Principal{}, fmt.Errorf("%w: expected two segments (type:id)", ErrInvalid)
	}

	p := Principal{
		Type:    PrincipalType(parts[0]),
		ID:      parts[1],
		checked: false,
		err:     nil,
	}

	if err := p.validate(); err != nil {
		return Principal{}, err
	}

	return p, nil
}

func (u Principal) IsZero() bool {
	return u.Type == "" && u.ID == ""
}

func (u Principal) String() string {
	return string(u.Type) + delimiter + u.ID
}

func (u Principal) Label() string {
	if u.Type == "" || u.ID == "" {
		return u.String()
	}
	return fmt.Sprintf("%s %q", u.Type, u.ID)
}

func (u Principal) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("principal urn to json: %w", err)
	}

	return b, nil
}

func (u *Principal) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read principal urn string from json: %w", err)
	}

	parsed, err := ParsePrincipal(s)
	if err != nil {
		return fmt.Errorf("parse principal urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *Principal) Scan(value any) error {
	if value == nil {
		return nil
	}

	var s string
	switch v := value.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	default:
		return fmt.Errorf("cannot scan %T into Principal", value)
	}

	parsed, err := ParsePrincipal(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u Principal) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u Principal) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal principal urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *Principal) UnmarshalText(text []byte) error {
	parsed, err := ParsePrincipal(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal principal urn text: %w", err)
	}

	*u = parsed

	return nil
}

// validate checks that the principal has a known type and a well-formed ID.
// For user and role principals the ID is intentionally permissive (any
// non-empty string up to maxSegmentLength) because IDs come from external
// systems (WorkOS) and do not follow the slug pattern. Agent IDs must be
// canonical UUIDs. Email IDs must be bare, lowercase RFC 5321 addresses so two
// assignments to the same person collapse to one row.
func (u *Principal) validate() error {
	if u.checked {
		return u.err
	}

	u.checked = true

	if u.Type == "" {
		u.err = fmt.Errorf("%w: empty type", ErrInvalid)
		return u.err
	}

	if _, ok := principalTypes[u.Type]; !ok {
		u.err = fmt.Errorf("%w: unknown principal type: %q", ErrInvalid, u.Type)
		return u.err
	}

	if u.ID == "" {
		u.err = fmt.Errorf("%w: empty id", ErrInvalid)
		return u.err
	}

	if len(u.ID) > maxSegmentLength {
		u.err = fmt.Errorf("%w: id segment is too long (max %d, got %d)", ErrInvalid, maxSegmentLength, len(u.ID))
		return u.err
	}

	if u.Type == PrincipalTypeAgent {
		id, err := uuid.Parse(u.ID)
		if err != nil || id.String() != u.ID {
			u.err = fmt.Errorf("%w: agent principal id must be a canonical UUID", ErrInvalid)
			return u.err
		}
	}

	if u.Type == PrincipalTypeSystem && !systemPrincipalIDPattern.MatchString(u.ID) {
		u.err = fmt.Errorf("%w: system principal id must be a lowercase kebab-case component name", ErrInvalid)
		return u.err
	}

	if u.Type == PrincipalTypeEmail {
		if u.ID != strings.ToLower(u.ID) {
			u.err = fmt.Errorf("%w: email principal id must be lowercase", ErrInvalid)
			return u.err
		}
		addr, err := mail.ParseAddress(u.ID)
		if err != nil {
			u.err = fmt.Errorf("%w: invalid email principal id: %w", ErrInvalid, err)
			return u.err
		}
		if addr.Address != u.ID || addr.Name != "" {
			u.err = fmt.Errorf("%w: email principal id must be the bare address", ErrInvalid)
			return u.err
		}
	}

	return nil
}
