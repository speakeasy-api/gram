package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type SkillShareLink struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewSkillShareLink(id uuid.UUID) SkillShareLink {
	s := SkillShareLink{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = s.validate()

	return s
}

func ParseSkillShareLink(value string) (SkillShareLink, error) {
	if value == "" {
		return SkillShareLink{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return SkillShareLink{}, fmt.Errorf("%w: expected two segments (skill-share-link:<uuid>)", ErrInvalid)
	}

	if parts[0] != "skill-share-link" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return SkillShareLink{}, fmt.Errorf("%w: expected skill-share-link urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return SkillShareLink{}, fmt.Errorf("%w: invalid skill-share-link uuid", ErrInvalid)
	}

	return NewSkillShareLink(id), nil
}

func (u SkillShareLink) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u SkillShareLink) String() string {
	return "skill-share-link" + delimiter + u.ID.String()
}

func (u SkillShareLink) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("skill-share-link urn to json: %w", err)
	}

	return b, nil
}

func (u *SkillShareLink) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read skill-share-link urn string from json: %w", err)
	}

	parsed, err := ParseSkillShareLink(s)
	if err != nil {
		return fmt.Errorf("parse skill-share-link urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *SkillShareLink) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into SkillShareLink", value)
	}

	parsed, err := ParseSkillShareLink(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u SkillShareLink) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u SkillShareLink) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal skill-share-link urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *SkillShareLink) UnmarshalText(text []byte) error {
	parsed, err := ParseSkillShareLink(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal skill-share-link urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *SkillShareLink) validate() error {
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
