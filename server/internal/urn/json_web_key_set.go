package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// JsonWebKeySet is a 2-segment URN identifying a json_web_key_sets row.
// Format: "json_web_key_set:<uuid>".
type JsonWebKeySet struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewJsonWebKeySet(id uuid.UUID) JsonWebKeySet {
	a := JsonWebKeySet{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseJsonWebKeySet(value string) (JsonWebKeySet, error) {
	if value == "" {
		return JsonWebKeySet{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return JsonWebKeySet{}, fmt.Errorf("%w: expected two segments (json_web_key_set:<uuid>)", ErrInvalid)
	}

	if parts[0] != "json_web_key_set" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return JsonWebKeySet{}, fmt.Errorf("%w: expected json_web_key_set urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return JsonWebKeySet{}, fmt.Errorf("%w: invalid json_web_key_set uuid", ErrInvalid)
	}

	return NewJsonWebKeySet(id), nil
}

func (u JsonWebKeySet) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u JsonWebKeySet) String() string {
	return "json_web_key_set" + delimiter + u.ID.String()
}

func (u JsonWebKeySet) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("json_web_key_set urn to json: %w", err)
	}

	return b, nil
}

func (u *JsonWebKeySet) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read json_web_key_set urn string from json: %w", err)
	}

	parsed, err := ParseJsonWebKeySet(s)
	if err != nil {
		return fmt.Errorf("parse json_web_key_set urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *JsonWebKeySet) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into JsonWebKeySet", value)
	}

	parsed, err := ParseJsonWebKeySet(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u JsonWebKeySet) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u JsonWebKeySet) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal json_web_key_set urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *JsonWebKeySet) UnmarshalText(text []byte) error {
	parsed, err := ParseJsonWebKeySet(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal json_web_key_set urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *JsonWebKeySet) validate() error {
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
