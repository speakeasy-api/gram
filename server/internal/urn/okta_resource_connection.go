package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type OktaResourceConnection struct {
	ID uuid.UUID
}

func NewOktaResourceConnection(id uuid.UUID) OktaResourceConnection {
	return OktaResourceConnection{ID: id}
}

func ParseOktaResourceConnection(value string) (OktaResourceConnection, error) {
	if value == "" {
		return OktaResourceConnection{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return OktaResourceConnection{}, fmt.Errorf("%w: expected two segments (okta_resource_connection:<uuid>)", ErrInvalid)
	}

	if parts[0] != "okta_resource_connection" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return OktaResourceConnection{}, fmt.Errorf("%w: expected okta_resource_connection urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return OktaResourceConnection{}, fmt.Errorf("%w: invalid okta_resource_connection uuid", ErrInvalid)
	}
	if id == uuid.Nil {
		return OktaResourceConnection{}, fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return NewOktaResourceConnection(id), nil
}

func (u OktaResourceConnection) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u OktaResourceConnection) String() string {
	return "okta_resource_connection" + delimiter + u.ID.String()
}

func (u OktaResourceConnection) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("okta_resource_connection urn to json: %w", err)
	}

	return b, nil
}

func (u *OktaResourceConnection) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read okta_resource_connection urn string from json: %w", err)
	}

	parsed, err := ParseOktaResourceConnection(s)
	if err != nil {
		return fmt.Errorf("parse okta_resource_connection urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *OktaResourceConnection) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into OktaResourceConnection", value)
	}

	parsed, err := ParseOktaResourceConnection(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u OktaResourceConnection) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u OktaResourceConnection) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal okta_resource_connection urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *OktaResourceConnection) UnmarshalText(text []byte) error {
	parsed, err := ParseOktaResourceConnection(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal okta_resource_connection urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u OktaResourceConnection) validate() error {
	if u.ID == uuid.Nil {
		return fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return nil
}
