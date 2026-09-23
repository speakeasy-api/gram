package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type SigintSensor struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewSigintSensor(id uuid.UUID) SigintSensor {
	u := SigintSensor{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = u.validate()

	return u
}

func ParseSigintSensor(value string) (SigintSensor, error) {
	if value == "" {
		return SigintSensor{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return SigintSensor{}, fmt.Errorf("%w: expected two segments (sigint-sensor:<uuid>)", ErrInvalid)
	}

	if parts[0] != "sigint-sensor" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return SigintSensor{}, fmt.Errorf("%w: expected sigint-sensor urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return SigintSensor{}, fmt.Errorf("%w: invalid sigint-sensor uuid", ErrInvalid)
	}

	return NewSigintSensor(id), nil
}

func (u SigintSensor) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u SigintSensor) String() string {
	return "sigint-sensor" + delimiter + u.ID.String()
}

func (u SigintSensor) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("sigint-sensor urn to json: %w", err)
	}

	return b, nil
}

func (u *SigintSensor) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read sigint-sensor urn string from json: %w", err)
	}

	parsed, err := ParseSigintSensor(s)
	if err != nil {
		return fmt.Errorf("parse sigint-sensor urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *SigintSensor) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into SigintSensor", value)
	}

	parsed, err := ParseSigintSensor(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u SigintSensor) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u SigintSensor) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal sigint-sensor urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *SigintSensor) UnmarshalText(text []byte) error {
	parsed, err := ParseSigintSensor(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal sigint-sensor urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *SigintSensor) validate() error {
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
