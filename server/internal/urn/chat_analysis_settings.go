package urn

import (
	"database/sql/driver"
)

const chatAnalysisSettingsPrefix = "chat_analysis_settings"

type ChatAnalysisSettings struct {
	ID string
}

func NewChatAnalysisSettings(organizationID string) ChatAnalysisSettings {
	return ChatAnalysisSettings{ID: organizationID}
}

func ParseChatAnalysisSettings(value string) (ChatAnalysisSettings, error) {
	id, err := settingsURNParse(chatAnalysisSettingsPrefix, value)
	if err != nil {
		return ChatAnalysisSettings{}, err
	}
	return ChatAnalysisSettings{ID: id}, nil
}

func (u ChatAnalysisSettings) IsZero() bool {
	return u.ID == ""
}

func (u ChatAnalysisSettings) String() string {
	return settingsURNString(chatAnalysisSettingsPrefix, u.ID)
}

func (u ChatAnalysisSettings) MarshalJSON() ([]byte, error) {
	return settingsURNMarshalJSON(chatAnalysisSettingsPrefix, u.ID)
}

func (u *ChatAnalysisSettings) UnmarshalJSON(data []byte) error {
	id, err := settingsURNUnmarshalJSON(chatAnalysisSettingsPrefix, data)
	if err != nil {
		return err
	}
	u.ID = id
	return nil
}

func (u *ChatAnalysisSettings) Scan(value any) error {
	if value == nil {
		return nil
	}
	id, err := settingsURNScan(chatAnalysisSettingsPrefix, "ChatAnalysisSettings", value)
	if err != nil {
		return err
	}
	u.ID = id
	return nil
}

func (u ChatAnalysisSettings) Value() (driver.Value, error) {
	return settingsURNValue(chatAnalysisSettingsPrefix, u.ID)
}

func (u ChatAnalysisSettings) MarshalText() ([]byte, error) {
	return settingsURNMarshalText(chatAnalysisSettingsPrefix, u.ID)
}

func (u *ChatAnalysisSettings) UnmarshalText(text []byte) error {
	id, err := settingsURNUnmarshalText(chatAnalysisSettingsPrefix, text)
	if err != nil {
		return err
	}
	u.ID = id
	return nil
}
