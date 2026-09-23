package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type SigintSignal struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewSigintSignal(id uuid.UUID) SigintSignal {
	u := SigintSignal{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = u.validate()

	return u
}

func ParseSigintSignal(value string) (SigintSignal, error) {
	if value == "" {
		return SigintSignal{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return SigintSignal{}, fmt.Errorf("%w: expected two segments (sigint-signal:<uuid>)", ErrInvalid)
	}

	if parts[0] != "sigint-signal" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return SigintSignal{}, fmt.Errorf("%w: expected sigint-signal urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return SigintSignal{}, fmt.Errorf("%w: invalid sigint-signal uuid", ErrInvalid)
	}

	return NewSigintSignal(id), nil
}

func (u SigintSignal) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u SigintSignal) String() string {
	return "sigint-signal" + delimiter + u.ID.String()
}

func (u SigintSignal) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("sigint-signal urn to json: %w", err)
	}

	return b, nil
}

func (u *SigintSignal) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read sigint-signal urn string from json: %w", err)
	}

	parsed, err := ParseSigintSignal(s)
	if err != nil {
		return fmt.Errorf("parse sigint-signal urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *SigintSignal) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into SigintSignal", value)
	}

	parsed, err := ParseSigintSignal(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u SigintSignal) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u SigintSignal) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal sigint-signal urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *SigintSignal) UnmarshalText(text []byte) error {
	parsed, err := ParseSigintSignal(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal sigint-signal urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *SigintSignal) validate() error {
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
