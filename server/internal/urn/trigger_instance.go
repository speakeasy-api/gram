package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type TriggerInstance struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewTriggerInstance(id uuid.UUID) TriggerInstance {
	a := TriggerInstance{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseTriggerInstance(value string) (TriggerInstance, error) {
	if value == "" {
		return TriggerInstance{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return TriggerInstance{}, fmt.Errorf("%w: expected two segments (trigger-instance:<uuid>)", ErrInvalid)
	}

	if parts[0] != "trigger-instance" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return TriggerInstance{}, fmt.Errorf("%w: expected trigger-instance urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return TriggerInstance{}, fmt.Errorf("%w: invalid trigger-instance uuid", ErrInvalid)
	}

	return NewTriggerInstance(id), nil
}

func (u TriggerInstance) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u TriggerInstance) String() string {
	return "trigger-instance" + delimiter + u.ID.String()
}

func (u TriggerInstance) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("trigger-instance urn to json: %w", err)
	}

	return b, nil
}

func (u *TriggerInstance) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read trigger-instance urn string from json: %w", err)
	}

	parsed, err := ParseTriggerInstance(s)
	if err != nil {
		return fmt.Errorf("parse trigger-instance urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *TriggerInstance) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into TriggerInstance", value)
	}

	parsed, err := ParseTriggerInstance(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u TriggerInstance) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u TriggerInstance) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal trigger-instance urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *TriggerInstance) UnmarshalText(text []byte) error {
	parsed, err := ParseTriggerInstance(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal trigger-instance urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *TriggerInstance) validate() error {
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
