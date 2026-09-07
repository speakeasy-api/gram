package urn

import (
	"database/sql/driver"
)

const openRouterAPIKeyPrefix = "openrouter_api_key"

// OpenRouterAPIKey identifies an organization's platform OpenRouter key row.
// The table is keyed by (organization_id, key_type), so the URN id is the two
// values joined with a slash (e.g. "org_123/chat").
type OpenRouterAPIKey struct {
	ID string
}

func NewOpenRouterAPIKey(organizationID string, keyType string) OpenRouterAPIKey {
	return OpenRouterAPIKey{ID: organizationID + "/" + keyType}
}

func ParseOpenRouterAPIKey(value string) (OpenRouterAPIKey, error) {
	id, err := settingsURNParse(openRouterAPIKeyPrefix, value)
	if err != nil {
		return OpenRouterAPIKey{}, err
	}
	return OpenRouterAPIKey{ID: id}, nil
}

func (u OpenRouterAPIKey) IsZero() bool {
	return u.ID == ""
}

func (u OpenRouterAPIKey) String() string {
	return settingsURNString(openRouterAPIKeyPrefix, u.ID)
}

func (u OpenRouterAPIKey) MarshalJSON() ([]byte, error) {
	return settingsURNMarshalJSON(openRouterAPIKeyPrefix, u.ID)
}

func (u *OpenRouterAPIKey) UnmarshalJSON(data []byte) error {
	id, err := settingsURNUnmarshalJSON(openRouterAPIKeyPrefix, data)
	if err != nil {
		return err
	}
	u.ID = id
	return nil
}

func (u *OpenRouterAPIKey) Scan(value any) error {
	if value == nil {
		return nil
	}
	id, err := settingsURNScan(openRouterAPIKeyPrefix, "OpenRouterAPIKey", value)
	if err != nil {
		return err
	}
	u.ID = id
	return nil
}

func (u OpenRouterAPIKey) Value() (driver.Value, error) {
	return settingsURNValue(openRouterAPIKeyPrefix, u.ID)
}

func (u OpenRouterAPIKey) MarshalText() ([]byte, error) {
	return settingsURNMarshalText(openRouterAPIKeyPrefix, u.ID)
}

func (u *OpenRouterAPIKey) UnmarshalText(text []byte) error {
	id, err := settingsURNUnmarshalText(openRouterAPIKeyPrefix, text)
	if err != nil {
		return err
	}
	u.ID = id
	return nil
}
