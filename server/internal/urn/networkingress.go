package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type NetworkIngress struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewNetworkIngress(id uuid.UUID) NetworkIngress {
	u := NetworkIngress{
		ID:      id,
		checked: false,
		err:     nil,
	}
	_ = u.validate()
	return u
}

func ParseNetworkIngress(value string) (NetworkIngress, error) {
	if value == "" {
		return NetworkIngress{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}
	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return NetworkIngress{}, fmt.Errorf("%w: expected two segments (netingress:<uuid>)", ErrInvalid)
	}
	if parts[0] != "netingress" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return NetworkIngress{}, fmt.Errorf("%w: expected network ingress urn (got: %q)", ErrInvalid, truncated)
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return NetworkIngress{}, fmt.Errorf("%w: invalid network ingress uuid", ErrInvalid)
	}
	return NewNetworkIngress(id), nil
}

func (u NetworkIngress) IsZero() bool   { return u.ID == uuid.Nil }
func (u NetworkIngress) String() string { return "netingress" + delimiter + u.ID.String() }

func (u NetworkIngress) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("network ingress urn to json: %w", err)
	}
	return encoded, nil
}
func (u *NetworkIngress) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("read network ingress urn string from json: %w", err)
	}
	parsed, err := ParseNetworkIngress(value)
	if err != nil {
		return fmt.Errorf("parse network ingress urn json string: %w", err)
	}
	*u = parsed
	return nil
}
func (u *NetworkIngress) Scan(value any) error {
	if value == nil {
		return nil
	}
	var raw string
	switch v := value.(type) {
	case string:
		raw = v
	case []byte:
		raw = string(v)
	default:
		return fmt.Errorf("cannot scan %T into NetworkIngress", value)
	}
	parsed, err := ParseNetworkIngress(raw)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}
	*u = parsed
	return nil
}
func (u NetworkIngress) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}
	return u.String(), nil
}
func (u NetworkIngress) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal network ingress urn text: %w", err)
	}
	return []byte(u.String()), nil
}
func (u *NetworkIngress) UnmarshalText(text []byte) error {
	parsed, err := ParseNetworkIngress(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal network ingress urn text: %w", err)
	}
	*u = parsed
	return nil
}
func (u *NetworkIngress) validate() error {
	if u.checked {
		return u.err
	}
	u.checked = true
	if u.ID == uuid.Nil {
		u.err = fmt.Errorf("%w: empty id", ErrInvalid)
	}
	return u.err
}
