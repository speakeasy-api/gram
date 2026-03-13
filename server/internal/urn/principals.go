package urn

import (
	"database/sql/driver"
	"fmt"
	"strings"
)

// PrincipalType is the type prefix of a principal URN.
type PrincipalType string

const (
	PrincipalTypeUser PrincipalType = "user"
	PrincipalTypeRole PrincipalType = "role"
)

var principalTypes = map[PrincipalType]struct{}{
	PrincipalTypeUser: {},
	PrincipalTypeRole: {},
}

// Principal is a typed identifier for an RBAC principal, stored in the database
// as "type:id" (e.g. "user:user_abc", "role:admin").
type Principal struct {
	Type PrincipalType
	ID   string
}

// NewPrincipal constructs a Principal and validates it eagerly.
func NewPrincipal(typ PrincipalType, id string) (Principal, error) {
	p := Principal{
		Type: typ,
		ID:   id,
	}
	if err := p.validate(); err != nil {
		return Principal{}, err
	}
	return p, nil
}

// ParsePrincipal parses a "type:id" string into a Principal.
func ParsePrincipal(value string) (Principal, error) {
	if value == "" {
		return Principal{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	typ, id, ok := strings.Cut(value, delimiter)
	if !ok {
		return Principal{}, fmt.Errorf("%w: expected type:id format", ErrInvalid)
	}

	p := Principal{
		Type: PrincipalType(typ),
		ID:   id,
	}
	if err := p.validate(); err != nil {
		return Principal{}, err
	}
	return p, nil
}

func (p Principal) IsZero() bool {
	return p.Type == "" && p.ID == ""
}

func (p Principal) String() string {
	return string(p.Type) + delimiter + p.ID
}

// Scan implements sql.Scanner for reading from the database.
func (p *Principal) Scan(value any) error {
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

	*p = parsed
	return nil
}

// Value implements driver.Valuer for writing to the database.
func (p Principal) Value() (driver.Value, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	return p.String(), nil
}

func (p Principal) validate() error {
	if p.Type == "" {
		return fmt.Errorf("%w: empty principal type", ErrInvalid)
	}
	if _, ok := principalTypes[p.Type]; !ok {
		return fmt.Errorf("%w: unknown principal type: %q", ErrInvalid, p.Type)
	}
	if p.ID == "" {
		return fmt.Errorf("%w: empty principal id", ErrInvalid)
	}
	if len(p.ID) > maxSegmentLength {
		return fmt.Errorf("%w: principal id too long (max %d, got %d)", ErrInvalid, maxSegmentLength, len(p.ID))
	}
	return nil
}
