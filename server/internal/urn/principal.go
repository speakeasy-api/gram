package urn

import (
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/mail"
	"strings"

	"github.com/google/uuid"
)

// PrincipalType represents the type prefix of a principal URN.
type PrincipalType string

const (
	PrincipalTypeUser               PrincipalType = "user"
	PrincipalTypeRole               PrincipalType = "role"
	PrincipalTypeEmail              PrincipalType = "email"
	PrincipalTypeAgent              PrincipalType = "agent"
	PrincipalTypeDirectoryGroup     PrincipalType = "directory_group"
	PrincipalTypeDirectoryAttribute PrincipalType = "directory_attribute"
)

// PrincipalWildcard is the URN that matches any principal in the org. It is
// not a typed `type:id` Principal — assignment tables store this literal
// alongside the typed URNs, so consumers special-case it.
const PrincipalWildcard = "*"

// AllUsersPrincipalID is reserved for the user:all subject-set principal.
// It must not be resolved as a concrete Gram user ID.
const AllUsersPrincipalID = "all"

// directoryAttributeIDMaxLength is larger than maxSegmentLength because the
// ID is base64url(key)+":"+base64url(value) from identity-provider attributes.
const directoryAttributeIDMaxLength = 512

var principalTypes = map[PrincipalType]struct{}{
	PrincipalTypeUser:               {},
	PrincipalTypeRole:               {},
	PrincipalTypeEmail:              {},
	PrincipalTypeAgent:              {},
	PrincipalTypeDirectoryGroup:     {},
	PrincipalTypeDirectoryAttribute: {},
}

// Principal is a 2-segment URN that identifies a principal in the RBAC system.
// Format: "type:id" where type is "user", "role", "email", "agent",
// "directory_group", or "directory_attribute" and id is the principal
// identifier (e.g. "user:user_01abc", "user:all", "role:admin",
// "email:dev@example.com", "agent:<uuid>", "directory_group:<uuid>", or
// "directory_attribute:<b64key>:<b64value>").
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
// systems (WorkOS) and do not follow the slug pattern. Agent and directory
// group IDs must be canonical UUIDs. Directory attribute IDs are
// base64url(key):base64url(value). Email IDs must be bare, lowercase RFC 5321
// addresses so two assignments to the same person collapse to one row.
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

	idLimit := maxSegmentLength
	if u.Type == PrincipalTypeDirectoryAttribute {
		idLimit = directoryAttributeIDMaxLength
	}
	if len(u.ID) > idLimit {
		u.err = fmt.Errorf("%w: id segment is too long (max %d, got %d)", ErrInvalid, idLimit, len(u.ID))
		return u.err
	}

	if u.Type == PrincipalTypeDirectoryGroup {
		id, err := uuid.Parse(u.ID)
		if err != nil || id.String() != u.ID {
			u.err = fmt.Errorf("%w: directory group principal id must be a canonical UUID", ErrInvalid)
			return u.err
		}
	}

	if u.Type == PrincipalTypeDirectoryAttribute {
		key, value, ok := strings.Cut(u.ID, ":")
		if !ok || key == "" || value == "" {
			u.err = fmt.Errorf("%w: directory attribute principal id must be key:value", ErrInvalid)
			return u.err
		}
		if _, err := base64.RawURLEncoding.DecodeString(key); err != nil {
			u.err = fmt.Errorf("%w: directory attribute principal key", ErrInvalid)
			return u.err
		}
		if _, err := base64.RawURLEncoding.DecodeString(value); err != nil {
			u.err = fmt.Errorf("%w: directory attribute principal value", ErrInvalid)
			return u.err
		}
	}

	if u.Type == PrincipalTypeAgent {
		id, err := uuid.Parse(u.ID)
		if err != nil || id.String() != u.ID {
			u.err = fmt.Errorf("%w: agent principal id must be a canonical UUID", ErrInvalid)
			return u.err
		}
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
