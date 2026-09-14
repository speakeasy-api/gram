package urn

import (
	"database/sql/driver"
)

const deviceAgentAiScanTargetPrefix = "device_agent_ai_scan_target"

// DeviceAgentAiScanTarget identifies a Shadow AI scan target in an
// organization's list. The table is keyed by (organization_id, id), so the
// URN id is the two values joined with a slash (e.g. "org_123/chatgpt").
type DeviceAgentAiScanTarget struct {
	ID string
}

func NewDeviceAgentAiScanTarget(organizationID string, targetID string) DeviceAgentAiScanTarget {
	return DeviceAgentAiScanTarget{ID: organizationID + "/" + targetID}
}

func ParseDeviceAgentAiScanTarget(value string) (DeviceAgentAiScanTarget, error) {
	id, err := settingsURNParse(deviceAgentAiScanTargetPrefix, value)
	if err != nil {
		return DeviceAgentAiScanTarget{}, err
	}
	return DeviceAgentAiScanTarget{ID: id}, nil
}

func (u DeviceAgentAiScanTarget) IsZero() bool {
	return u.ID == ""
}

func (u DeviceAgentAiScanTarget) String() string {
	return settingsURNString(deviceAgentAiScanTargetPrefix, u.ID)
}

func (u DeviceAgentAiScanTarget) MarshalJSON() ([]byte, error) {
	return settingsURNMarshalJSON(deviceAgentAiScanTargetPrefix, u.ID)
}

func (u *DeviceAgentAiScanTarget) UnmarshalJSON(data []byte) error {
	id, err := settingsURNUnmarshalJSON(deviceAgentAiScanTargetPrefix, data)
	if err != nil {
		return err
	}
	u.ID = id
	return nil
}

func (u *DeviceAgentAiScanTarget) Scan(value any) error {
	if value == nil {
		return nil
	}
	id, err := settingsURNScan(deviceAgentAiScanTargetPrefix, "DeviceAgentAiScanTarget", value)
	if err != nil {
		return err
	}
	u.ID = id
	return nil
}

func (u DeviceAgentAiScanTarget) Value() (driver.Value, error) {
	return settingsURNValue(deviceAgentAiScanTargetPrefix, u.ID)
}

func (u DeviceAgentAiScanTarget) MarshalText() ([]byte, error) {
	return settingsURNMarshalText(deviceAgentAiScanTargetPrefix, u.ID)
}

func (u *DeviceAgentAiScanTarget) UnmarshalText(text []byte) error {
	id, err := settingsURNUnmarshalText(deviceAgentAiScanTargetPrefix, text)
	if err != nil {
		return err
	}
	u.ID = id
	return nil
}
