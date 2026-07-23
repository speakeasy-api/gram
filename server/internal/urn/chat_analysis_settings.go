package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
)

type ChatAnalysisSettings struct {
	ID string
}

func NewChatAnalysisSettings(organizationID string) ChatAnalysisSettings {
	return ChatAnalysisSettings{ID: organizationID}
}

func ParseChatAnalysisSettings(value string) (ChatAnalysisSettings, error) {
	if value == "" {
		return ChatAnalysisSettings{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return ChatAnalysisSettings{}, fmt.Errorf("%w: expected two segments (chat_analysis_settings:<organization_id>)", ErrInvalid)
	}
	if parts[0] != "chat_analysis_settings" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return ChatAnalysisSettings{}, fmt.Errorf("%w: expected chat_analysis_settings urn (got: %q)", ErrInvalid, truncated)
	}

	parsed := NewChatAnalysisSettings(parts[1])
	if err := parsed.validate(); err != nil {
		return ChatAnalysisSettings{}, err
	}
	return parsed, nil
}

func (u ChatAnalysisSettings) IsZero() bool {
	return u.ID == ""
}

func (u ChatAnalysisSettings) String() string {
	return "chat_analysis_settings" + delimiter + u.ID
}

func (u ChatAnalysisSettings) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("chat_analysis_settings urn to json: %w", err)
	}
	return b, nil
}

func (u *ChatAnalysisSettings) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("read chat_analysis_settings urn string from json: %w", err)
	}

	parsed, err := ParseChatAnalysisSettings(value)
	if err != nil {
		return fmt.Errorf("parse chat_analysis_settings urn json string: %w", err)
	}
	*u = parsed
	return nil
}

func (u *ChatAnalysisSettings) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into ChatAnalysisSettings", value)
	}

	parsed, err := ParseChatAnalysisSettings(text)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}
	*u = parsed
	return nil
}

func (u ChatAnalysisSettings) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}
	return u.String(), nil
}

func (u ChatAnalysisSettings) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal chat_analysis_settings urn text: %w", err)
	}
	return []byte(u.String()), nil
}

func (u *ChatAnalysisSettings) UnmarshalText(text []byte) error {
	parsed, err := ParseChatAnalysisSettings(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal chat_analysis_settings urn text: %w", err)
	}
	*u = parsed
	return nil
}

func (u ChatAnalysisSettings) validate() error {
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
