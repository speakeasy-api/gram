package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type DeviceIntegrationConfig struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewDeviceIntegrationConfig(id uuid.UUID) DeviceIntegrationConfig {
	c := DeviceIntegrationConfig{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = c.validate()

	return c
}

func ParseDeviceIntegrationConfig(value string) (DeviceIntegrationConfig, error) {
	if value == "" {
		return DeviceIntegrationConfig{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return DeviceIntegrationConfig{}, fmt.Errorf("%w: expected two segments (device_integration_config:<uuid>)", ErrInvalid)
	}

	if parts[0] != "device_integration_config" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return DeviceIntegrationConfig{}, fmt.Errorf("%w: expected device_integration_config urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return DeviceIntegrationConfig{}, fmt.Errorf("%w: invalid device_integration_config uuid", ErrInvalid)
	}

	return NewDeviceIntegrationConfig(id), nil
}

func (u DeviceIntegrationConfig) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u DeviceIntegrationConfig) String() string {
	return "device_integration_config" + delimiter + u.ID.String()
}

func (u DeviceIntegrationConfig) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("device_integration_config urn to json: %w", err)
	}

	return b, nil
}

func (u *DeviceIntegrationConfig) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read device_integration_config urn string from json: %w", err)
	}

	parsed, err := ParseDeviceIntegrationConfig(s)
	if err != nil {
		return fmt.Errorf("parse device_integration_config urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *DeviceIntegrationConfig) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into DeviceIntegrationConfig", value)
	}

	parsed, err := ParseDeviceIntegrationConfig(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u DeviceIntegrationConfig) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u DeviceIntegrationConfig) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal device_integration_config urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *DeviceIntegrationConfig) UnmarshalText(text []byte) error {
	parsed, err := ParseDeviceIntegrationConfig(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal device_integration_config urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *DeviceIntegrationConfig) validate() error {
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
