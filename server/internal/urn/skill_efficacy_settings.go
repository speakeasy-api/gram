package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
)

type SkillEfficacySettings struct {
	ID string
}

func NewSkillEfficacySettings(organizationID string) SkillEfficacySettings {
	return SkillEfficacySettings{ID: organizationID}
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

	parsed := NewSkillEfficacySettings(parts[1])
	if err := parsed.validate(); err != nil {
		return SkillEfficacySettings{}, err
	}
	return parsed, nil
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

func (u SkillEfficacySettings) validate() error {
	switch {
	case u.ID == "":
		return fmt.Errorf("%w: empty id", ErrInvalid)
	case len(u.ID) > maxSegmentLength:
		return fmt.Errorf("%w: id segment is too long (max %d, got %d)", ErrInvalid, maxSegmentLength, len(u.ID))
	case strings.Contains(u.ID, delimiter):
		return fmt.Errorf("%w: id contains delimiter", ErrInvalid)
	default:
		return nil
	}
}
