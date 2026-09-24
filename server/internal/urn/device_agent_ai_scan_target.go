package urn

import (
	"database/sql/driver"
)

const deviceAgentAiScanTargetPrefix = "device_agent_ai_scan_target"

// AiScanTarget identifies a Shadow AI scan target in an
// organization's list. The table is keyed by (organization_id, id), so the
// URN id is the two values joined with a slash (e.g. "org_123/chatgpt").
type AiScanTarget struct {
	ID string
}

func NewAiScanTarget(organizationID string, targetID string) AiScanTarget {
	return AiScanTarget{ID: organizationID + "/" + targetID}
}

func ParseAiScanTarget(value string) (AiScanTarget, error) {
	id, err := settingsURNParse(deviceAgentAiScanTargetPrefix, value)
	if err != nil {
		return AiScanTarget{}, err
	}
	return AiScanTarget{ID: id}, nil
}

func (u AiScanTarget) IsZero() bool {
	return u.ID == ""
}

func (u AiScanTarget) String() string {
	return settingsURNString(deviceAgentAiScanTargetPrefix, u.ID)
}

func (u AiScanTarget) MarshalJSON() ([]byte, error) {
	return settingsURNMarshalJSON(deviceAgentAiScanTargetPrefix, u.ID)
}

func (u *AiScanTarget) UnmarshalJSON(data []byte) error {
	id, err := settingsURNUnmarshalJSON(deviceAgentAiScanTargetPrefix, data)
	if err != nil {
		return err
	}
	u.ID = id
	return nil
}

func (u *AiScanTarget) Scan(value any) error {
	if value == nil {
		return nil
	}
	id, err := settingsURNScan(deviceAgentAiScanTargetPrefix, "AiScanTarget", value)
	if err != nil {
		return err
	}
	u.ID = id
	return nil
}

func (u AiScanTarget) Value() (driver.Value, error) {
	return settingsURNValue(deviceAgentAiScanTargetPrefix, u.ID)
}

func (u AiScanTarget) MarshalText() ([]byte, error) {
	return settingsURNMarshalText(deviceAgentAiScanTargetPrefix, u.ID)
}

func (u *AiScanTarget) UnmarshalText(text []byte) error {
	id, err := settingsURNUnmarshalText(deviceAgentAiScanTargetPrefix, text)
	if err != nil {
		return err
	}
	u.ID = id
	return nil
}
