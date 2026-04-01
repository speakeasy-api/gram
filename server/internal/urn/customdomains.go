package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type CustomDomain struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewCustomDomain(id uuid.UUID) CustomDomain {
	t := CustomDomain{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = t.validate()

	return t
}

func ParseCustomDomain(value string) (CustomDomain, error) {
	if value == "" {
		return CustomDomain{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return CustomDomain{}, fmt.Errorf("%w: expected two segments (customdomain:<uuid>)", ErrInvalid)
	}

	if parts[0] != "customdomain" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return CustomDomain{}, fmt.Errorf("%w: expected custom domain urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return CustomDomain{}, fmt.Errorf("%w: invalid custom domain uuid", ErrInvalid)
	}

	return NewCustomDomain(id), nil
}

func (u CustomDomain) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u CustomDomain) String() string {
	return "customdomain" + delimiter + u.ID.String()
}

func (u CustomDomain) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("custom domain urn to json: %w", err)
	}

	return b, nil
}

func (u *CustomDomain) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read custom domain urn string from json: %w", err)
	}

	parsed, err := ParseCustomDomain(s)
	if err != nil {
		return fmt.Errorf("parse custom domain urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *CustomDomain) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into CustomDomain", value)
	}

	parsed, err := ParseCustomDomain(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u CustomDomain) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u CustomDomain) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal custom domain urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *CustomDomain) UnmarshalText(text []byte) error {
	parsed, err := ParseCustomDomain(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal custom domain urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *CustomDomain) validate() error {
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
