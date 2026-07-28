package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type Skill struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewSkill(id uuid.UUID) Skill {
	s := Skill{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = s.validate()

	return s
}

func ParseSkill(value string) (Skill, error) {
	if value == "" {
		return Skill{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return Skill{}, fmt.Errorf("%w: expected two segments (skill:<uuid>)", ErrInvalid)
	}

	if parts[0] != "skill" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return Skill{}, fmt.Errorf("%w: expected skill urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return Skill{}, fmt.Errorf("%w: invalid skill uuid", ErrInvalid)
	}

	return NewSkill(id), nil
}

func (u Skill) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u Skill) String() string {
	return "skill" + delimiter + u.ID.String()
}

func (u Skill) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("skill urn to json: %w", err)
	}

	return b, nil
}

func (u *Skill) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read skill urn string from json: %w", err)
	}

	parsed, err := ParseSkill(s)
	if err != nil {
		return fmt.Errorf("parse skill urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *Skill) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into Skill", value)
	}

	parsed, err := ParseSkill(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u Skill) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u Skill) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal skill urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *Skill) UnmarshalText(text []byte) error {
	parsed, err := ParseSkill(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal skill urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *Skill) validate() error {
	if u.checked {
		return u.err
	}

	u.checked = true

	if u.ID == uuid.Nil {
		u.err = fmt.Errorf("%w: empty id", ErrInvalid)
		return u.err
	}

	return nil
}
