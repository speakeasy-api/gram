package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type Widget struct {
	ID uuid.UUID
}

func NewWidget(id uuid.UUID) Widget {
	return Widget{ID: id}
}

func ParseWidget(value string) (Widget, error) {
	if value == "" {
		return Widget{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return Widget{}, fmt.Errorf("%w: expected two segments (widget:<uuid>)", ErrInvalid)
	}

	if parts[0] != "widget" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return Widget{}, fmt.Errorf("%w: expected widget urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return Widget{}, fmt.Errorf("%w: invalid widget uuid", ErrInvalid)
	}

	// This type treats the nil uuid as invalid, so it must not survive a
	// parse and turn up as a valid-looking urn later.
	if id == uuid.Nil {
		return Widget{}, fmt.Errorf("%w: empty widget uuid", ErrInvalid)
	}

	return NewWidget(id), nil
}

func (u Widget) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u Widget) String() string {
	return "widget" + delimiter + u.ID.String()
}

func (u Widget) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("widget urn to json: %w", err)
	}

	return b, nil
}

func (u *Widget) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read widget urn string from json: %w", err)
	}

	parsed, err := ParseWidget(s)
	if err != nil {
		return fmt.Errorf("parse widget urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *Widget) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into Widget", value)
	}

	parsed, err := ParseWidget(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u Widget) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u Widget) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal widget urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *Widget) UnmarshalText(text []byte) error {
	parsed, err := ParseWidget(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal widget urn text: %w", err)
	}

	*u = parsed

	return nil
}

// validate reads the current ID every call. ID is exported and mutable, so a
// cached verdict could outlive the value it judged and report a zeroed urn as
// valid.
func (u *Widget) validate() error {
	if u.ID == uuid.Nil {
		return fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return nil
}
