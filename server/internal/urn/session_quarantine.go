package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

const sessionQuarantinePrefix = "session_quarantine"

type SessionQuarantine struct {
	ID uuid.UUID
}

func NewSessionQuarantine(id uuid.UUID) SessionQuarantine {
	return SessionQuarantine{ID: id}
}

func ParseSessionQuarantine(value string) (SessionQuarantine, error) {
	if value == "" {
		return SessionQuarantine{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return SessionQuarantine{}, fmt.Errorf("%w: expected two segments (%s:<uuid>)", ErrInvalid, sessionQuarantinePrefix)
	}
	if parts[0] != sessionQuarantinePrefix {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return SessionQuarantine{}, fmt.Errorf("%w: expected %s urn (got: %q)", ErrInvalid, sessionQuarantinePrefix, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return SessionQuarantine{}, fmt.Errorf("%w: invalid session_quarantine uuid", ErrInvalid)
	}
	if id == uuid.Nil {
		return SessionQuarantine{}, fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return NewSessionQuarantine(id), nil
}

func (u SessionQuarantine) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u SessionQuarantine) String() string {
	return sessionQuarantinePrefix + delimiter + u.ID.String()
}

func (u SessionQuarantine) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("session_quarantine urn to json: %w", err)
	}
	return b, nil
}

func (u *SessionQuarantine) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("read session_quarantine urn string from json: %w", err)
	}

	parsed, err := ParseSessionQuarantine(value)
	if err != nil {
		return fmt.Errorf("parse session_quarantine urn json string: %w", err)
	}
	*u = parsed
	return nil
}

func (u *SessionQuarantine) Scan(value any) error {
	if value == nil {
		return nil
	}

	var text string
	switch v := value.(type) {
	case string:
		text = v
	case []byte:
		text = string(v)
	default:
		return fmt.Errorf("cannot scan %T into SessionQuarantine", value)
	}

	parsed, err := ParseSessionQuarantine(text)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}
	*u = parsed
	return nil
}

func (u SessionQuarantine) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}
	return u.String(), nil
}

func (u SessionQuarantine) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal session_quarantine urn text: %w", err)
	}
	return []byte(u.String()), nil
}

func (u *SessionQuarantine) UnmarshalText(text []byte) error {
	parsed, err := ParseSessionQuarantine(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal session_quarantine urn text: %w", err)
	}
	*u = parsed
	return nil
}

func (u SessionQuarantine) validate() error {
	if u.ID == uuid.Nil {
		return fmt.Errorf("%w: empty id", ErrInvalid)
	}
	return nil
}
