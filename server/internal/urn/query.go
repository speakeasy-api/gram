package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type Query struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewQuery(id uuid.UUID) Query {
	a := Query{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseQuery(value string) (Query, error) {
	if value == "" {
		return Query{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return Query{}, fmt.Errorf("%w: expected two segments (query:<uuid>)", ErrInvalid)
	}

	if parts[0] != "query" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return Query{}, fmt.Errorf("%w: expected query urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return Query{}, fmt.Errorf("%w: invalid query uuid", ErrInvalid)
	}

	return NewQuery(id), nil
}

func (u Query) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u Query) String() string {
	return "query" + delimiter + u.ID.String()
}

func (u Query) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("query urn to json: %w", err)
	}

	return b, nil
}

func (u *Query) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read query urn string from json: %w", err)
	}

	parsed, err := ParseQuery(s)
	if err != nil {
		return fmt.Errorf("parse query urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *Query) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into Query", value)
	}

	parsed, err := ParseQuery(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u Query) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u Query) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal query urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *Query) UnmarshalText(text []byte) error {
	parsed, err := ParseQuery(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal query urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *Query) validate() error {
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
