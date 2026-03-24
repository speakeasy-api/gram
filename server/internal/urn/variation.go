package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type Variation struct {
	Kind VariationKind
	ID   uuid.UUID

	checked bool
	err     error
}

func NewVariation(kind VariationKind, id uuid.UUID) Variation {
	v := Variation{
		Kind:    kind,
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = v.validate()

	return v
}

func ParseVariation(value string) (Variation, error) {
	if value == "" {
		return Variation{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 3)
	if len(parts) != 3 || parts[1] == "" || parts[2] == "" || strings.Contains(parts[2], delimiter) {
		return Variation{}, fmt.Errorf("%w: expected three segments (variations:<kind>:<uuid>)", ErrInvalid)
	}

	if parts[0] != "variations" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return Variation{}, fmt.Errorf("%w: expected variations urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[2])
	if err != nil {
		return Variation{}, fmt.Errorf("%w: invalid variation uuid", ErrInvalid)
	}

	v := Variation{
		Kind:    VariationKind(parts[1]),
		ID:      id,
		checked: false,
		err:     nil,
	}

	if err := v.validate(); err != nil {
		return Variation{}, err
	}

	return v, nil
}

func (u Variation) IsZero() bool {
	return u.Kind == "" && u.ID == uuid.Nil
}

func (u Variation) String() string {
	return "variations" + delimiter + string(u.Kind) + delimiter + u.ID.String()
}

func (u Variation) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("variation urn to json: %w", err)
	}

	return b, nil
}

func (u *Variation) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read variation urn string from json: %w", err)
	}

	parsed, err := ParseVariation(s)
	if err != nil {
		return fmt.Errorf("parse variation urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *Variation) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into Variation", value)
	}

	parsed, err := ParseVariation(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u Variation) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u Variation) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal variation urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *Variation) UnmarshalText(text []byte) error {
	parsed, err := ParseVariation(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal variation urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *Variation) validate() error {
	if u.checked {
		return u.err
	}

	u.checked = true

	if _, ok := variationKinds[u.Kind]; !ok {
		u.err = fmt.Errorf("%w: unknown variation kind: %q", ErrInvalid, u.Kind)
		return u.err
	}

	if u.ID == uuid.Nil {
		u.err = fmt.Errorf("%w: empty id", ErrInvalid)
		return u.err
	}

	if u.ID.Version() != 7 {
		u.err = fmt.Errorf("%w: variation id must be uuid v7", ErrInvalid)
		return u.err
	}

	return nil
}
