package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type Dashboard struct {
	ID uuid.UUID
}

func NewDashboard(id uuid.UUID) Dashboard {
	return Dashboard{ID: id}
}

func ParseDashboard(value string) (Dashboard, error) {
	if value == "" {
		return Dashboard{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return Dashboard{}, fmt.Errorf("%w: expected two segments (dashboard:<uuid>)", ErrInvalid)
	}

	if parts[0] != "dashboard" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return Dashboard{}, fmt.Errorf("%w: expected dashboard urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return Dashboard{}, fmt.Errorf("%w: invalid dashboard uuid", ErrInvalid)
	}

	// This type treats the nil uuid as invalid, so it must not survive a
	// parse and turn up as a valid-looking urn later.
	if id == uuid.Nil {
		return Dashboard{}, fmt.Errorf("%w: empty dashboard uuid", ErrInvalid)
	}

	return NewDashboard(id), nil
}

func (u Dashboard) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u Dashboard) String() string {
	return "dashboard" + delimiter + u.ID.String()
}

func (u Dashboard) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("dashboard urn to json: %w", err)
	}

	return b, nil
}

func (u *Dashboard) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read dashboard urn string from json: %w", err)
	}

	parsed, err := ParseDashboard(s)
	if err != nil {
		return fmt.Errorf("parse dashboard urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *Dashboard) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into Dashboard", value)
	}

	parsed, err := ParseDashboard(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u Dashboard) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u Dashboard) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal dashboard urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *Dashboard) UnmarshalText(text []byte) error {
	parsed, err := ParseDashboard(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal dashboard urn text: %w", err)
	}

	*u = parsed

	return nil
}

// validate reads the current ID every call. ID is exported and mutable, so a
// cached verdict could outlive the value it judged and report a zeroed urn as
// valid.
func (u *Dashboard) validate() error {
	if u.ID == uuid.Nil {
		return fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return nil
}
