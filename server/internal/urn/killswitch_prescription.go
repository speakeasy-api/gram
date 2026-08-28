package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

const killswitchPrescriptionPrefix = "killswitch_prescription"

type KillswitchPrescription struct {
	ID uuid.UUID
}

func NewKillswitchPrescription(id uuid.UUID) KillswitchPrescription {
	return KillswitchPrescription{ID: id}
}

func ParseKillswitchPrescription(value string) (KillswitchPrescription, error) {
	if value == "" {
		return KillswitchPrescription{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return KillswitchPrescription{}, fmt.Errorf("%w: expected two segments (%s:<uuid>)", ErrInvalid, killswitchPrescriptionPrefix)
	}
	if parts[0] != killswitchPrescriptionPrefix {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return KillswitchPrescription{}, fmt.Errorf("%w: expected %s urn (got: %q)", ErrInvalid, killswitchPrescriptionPrefix, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return KillswitchPrescription{}, fmt.Errorf("%w: invalid killswitch prescription uuid", ErrInvalid)
	}
	if id == uuid.Nil {
		return KillswitchPrescription{}, fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return KillswitchPrescription{ID: id}, nil
}

func (u KillswitchPrescription) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u KillswitchPrescription) String() string {
	return killswitchPrescriptionPrefix + delimiter + u.ID.String()
}

func (u KillswitchPrescription) validate() error {
	if u.ID == uuid.Nil {
		return fmt.Errorf("%w: empty id", ErrInvalid)
	}
	return nil
}

func (u KillswitchPrescription) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("killswitch prescription urn to json: %w", err)
	}

	return b, nil
}

func (u *KillswitchPrescription) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read killswitch prescription urn string from json: %w", err)
	}

	parsed, err := ParseKillswitchPrescription(s)
	if err != nil {
		return fmt.Errorf("parse killswitch prescription urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *KillswitchPrescription) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into KillswitchPrescription", value)
	}

	parsed, err := ParseKillswitchPrescription(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u KillswitchPrescription) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u KillswitchPrescription) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal killswitch prescription urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *KillswitchPrescription) UnmarshalText(text []byte) error {
	parsed, err := ParseKillswitchPrescription(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal killswitch prescription urn text: %w", err)
	}

	*u = parsed

	return nil
}
