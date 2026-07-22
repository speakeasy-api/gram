package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
)

type SkillEfficacySettings struct {
	ID string

	checked bool
	err     error
}

func NewSkillEfficacySettings(organizationID string) SkillEfficacySettings {
	u := SkillEfficacySettings{ID: organizationID, checked: false, err: nil}
	_ = u.validate()
	return u
}

func ParseSkillEfficacySettings(value string) (SkillEfficacySettings, error) {
	if value == "" {
		return SkillEfficacySettings{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return SkillEfficacySettings{}, fmt.Errorf("%w: expected two segments (skill_efficacy_settings:<organization_id>)", ErrInvalid)
	}
	if parts[0] != "skill_efficacy_settings" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return SkillEfficacySettings{}, fmt.Errorf("%w: expected skill_efficacy_settings urn (got: %q)", ErrInvalid, truncated)
	}

	return NewSkillEfficacySettings(parts[1]), nil
}

func (u SkillEfficacySettings) IsZero() bool {
	return u.ID == ""
}

func (u SkillEfficacySettings) String() string {
	return "skill_efficacy_settings" + delimiter + u.ID
}

func (u SkillEfficacySettings) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("skill_efficacy_settings urn to json: %w", err)
	}
	return b, nil
}

func (u *SkillEfficacySettings) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("read skill_efficacy_settings urn string from json: %w", err)
	}

	parsed, err := ParseSkillEfficacySettings(value)
	if err != nil {
		return fmt.Errorf("parse skill_efficacy_settings urn json string: %w", err)
	}
	*u = parsed
	return nil
}

func (u *SkillEfficacySettings) Scan(value any) error {
	if value == nil {
		return nil
	}

	var text string
	switch v := value.(type) {
	case string:
		text = v
	case []byte:
		text = string(v)
	default:
		return fmt.Errorf("cannot scan %T into SkillEfficacySettings", value)
	}

	parsed, err := ParseSkillEfficacySettings(text)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}
	*u = parsed
	return nil
}

func (u SkillEfficacySettings) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}
	return u.String(), nil
}

func (u SkillEfficacySettings) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal skill_efficacy_settings urn text: %w", err)
	}
	return []byte(u.String()), nil
}

func (u *SkillEfficacySettings) UnmarshalText(text []byte) error {
	parsed, err := ParseSkillEfficacySettings(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal skill_efficacy_settings urn text: %w", err)
	}
	*u = parsed
	return nil
}

func (u *SkillEfficacySettings) validate() error {
	if u.checked {
		return u.err
	}
	u.checked = true

	switch {
	case u.ID == "":
		u.err = fmt.Errorf("%w: empty id", ErrInvalid)
	case strings.Contains(u.ID, delimiter):
		u.err = fmt.Errorf("%w: id contains delimiter", ErrInvalid)
	}
	return u.err
}
