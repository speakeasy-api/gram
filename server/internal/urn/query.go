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
}

func NewQuery(id uuid.UUID) Query {
	return Query{ID: id}
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

	// This type treats the nil uuid as invalid, so it must not survive a
	// parse and turn up as a valid-looking urn later.
	if id == uuid.Nil {
		return Query{}, fmt.Errorf("%w: empty query uuid", ErrInvalid)
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

// validate reads the current ID every call. ID is exported and mutable, so a
// cached verdict could outlive the value it judged and report a zeroed urn as
// valid.
func (u *Query) validate() error {
	if u.ID == uuid.Nil {
		return fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return nil
}
