package urn

import (
	"database/sql/driver"
)

const skillEfficacySettingsPrefix = "skill_efficacy_settings"

type SkillEfficacySettings struct {
	ID string
}

func NewSkillEfficacySettings(organizationID string) SkillEfficacySettings {
	return SkillEfficacySettings{ID: organizationID}
}

func ParseSkillEfficacySettings(value string) (SkillEfficacySettings, error) {
	id, err := settingsURNParse(skillEfficacySettingsPrefix, value)
	if err != nil {
		return SkillEfficacySettings{}, err
	}
	return SkillEfficacySettings{ID: id}, nil
}

func (u SkillEfficacySettings) IsZero() bool {
	return u.ID == ""
}

func (u SkillEfficacySettings) String() string {
	return settingsURNString(skillEfficacySettingsPrefix, u.ID)
}

func (u SkillEfficacySettings) MarshalJSON() ([]byte, error) {
	return settingsURNMarshalJSON(skillEfficacySettingsPrefix, u.ID)
}

func (u *SkillEfficacySettings) UnmarshalJSON(data []byte) error {
	id, err := settingsURNUnmarshalJSON(skillEfficacySettingsPrefix, data)
	if err != nil {
		return err
	}
	u.ID = id
	return nil
}

func (u *SkillEfficacySettings) Scan(value any) error {
	if value == nil {
		return nil
	}
	id, err := settingsURNScan(skillEfficacySettingsPrefix, "SkillEfficacySettings", value)
	if err != nil {
		return err
	}
	u.ID = id
	return nil
}

func (u SkillEfficacySettings) Value() (driver.Value, error) {
	return settingsURNValue(skillEfficacySettingsPrefix, u.ID)
}

func (u SkillEfficacySettings) MarshalText() ([]byte, error) {
	return settingsURNMarshalText(skillEfficacySettingsPrefix, u.ID)
}

func (u *SkillEfficacySettings) UnmarshalText(text []byte) error {
	id, err := settingsURNUnmarshalText(skillEfficacySettingsPrefix, text)
	if err != nil {
		return err
	}
	u.ID = id
	return nil
}
