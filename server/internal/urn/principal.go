package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
)

// PrincipalType represents the type prefix of a principal URN.
type PrincipalType string

const (
	PrincipalTypeUser   PrincipalType = "user"
	PrincipalTypeRole   PrincipalType = "role"
	PrincipalTypeAPIKey PrincipalType = "api_key"
)

var principalTypes = map[PrincipalType]struct{}{
	PrincipalTypeUser:   {},
	PrincipalTypeRole:   {},
	PrincipalTypeAPIKey: {},
}

// Principal is a 2-segment URN that identifies a principal in the RBAC system.
// Format: "type:id" where type is one of "user" or "role" and id is the
// principal identifier (e.g. "user:user_01abc", "role:admin").
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

// validate checks that the principal has a known type and a non-empty ID.
// The ID segment is intentionally permissive (any non-empty string up to
// maxSegmentLength) because user IDs come from external systems (WorkOS) and
// do not follow the slug pattern.
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

	return nil
}
