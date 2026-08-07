package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// PlatformMcpRegistration identifies a Platform MCP catalog registration.
type PlatformMcpRegistration struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewPlatformMcpRegistration(id uuid.UUID) PlatformMcpRegistration {
	registration := PlatformMcpRegistration{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = registration.validate()

	return registration
}

func ParsePlatformMcpRegistration(value string) (PlatformMcpRegistration, error) {
	if value == "" {
		return PlatformMcpRegistration{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return PlatformMcpRegistration{}, fmt.Errorf("%w: expected two segments (platform-mcp-registration:<uuid>)", ErrInvalid)
	}

	if parts[0] != "platform-mcp-registration" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return PlatformMcpRegistration{}, fmt.Errorf("%w: expected platform-mcp-registration urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return PlatformMcpRegistration{}, fmt.Errorf("%w: invalid platform-mcp-registration uuid", ErrInvalid)
	}

	return NewPlatformMcpRegistration(id), nil
}

func (u PlatformMcpRegistration) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u PlatformMcpRegistration) String() string {
	return "platform-mcp-registration" + delimiter + u.ID.String()
}

func (u PlatformMcpRegistration) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("platform-mcp-registration urn to json: %w", err)
	}

	return b, nil
}

func (u *PlatformMcpRegistration) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("read platform-mcp-registration urn string from json: %w", err)
	}

	parsed, err := ParsePlatformMcpRegistration(value)
	if err != nil {
		return fmt.Errorf("parse platform-mcp-registration urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *PlatformMcpRegistration) Scan(value any) error {
	if value == nil {
		return nil
	}

	var stringValue string
	switch value := value.(type) {
	case string:
		stringValue = value
	case []byte:
		stringValue = string(value)
	default:
		return fmt.Errorf("cannot scan %T into PlatformMcpRegistration", value)
	}

	parsed, err := ParsePlatformMcpRegistration(stringValue)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u PlatformMcpRegistration) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u PlatformMcpRegistration) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal platform-mcp-registration urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *PlatformMcpRegistration) UnmarshalText(text []byte) error {
	parsed, err := ParsePlatformMcpRegistration(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal platform-mcp-registration urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *PlatformMcpRegistration) validate() error {
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
