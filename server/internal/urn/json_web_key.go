package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// JsonWebKey is a 2-segment URN identifying a json_web_keys row (a published
// key inside a JSON Web Key Set). Format: "json_web_key:<uuid>".
type JsonWebKey struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewJsonWebKey(id uuid.UUID) JsonWebKey {
	a := JsonWebKey{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseJsonWebKey(value string) (JsonWebKey, error) {
	if value == "" {
		return JsonWebKey{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return JsonWebKey{}, fmt.Errorf("%w: expected two segments (json_web_key:<uuid>)", ErrInvalid)
	}

	if parts[0] != "json_web_key" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return JsonWebKey{}, fmt.Errorf("%w: expected json_web_key urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return JsonWebKey{}, fmt.Errorf("%w: invalid json_web_key uuid", ErrInvalid)
	}

	return NewJsonWebKey(id), nil
}

func (u JsonWebKey) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u JsonWebKey) String() string {
	return "json_web_key" + delimiter + u.ID.String()
}

func (u JsonWebKey) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("json_web_key urn to json: %w", err)
	}

	return b, nil
}

func (u *JsonWebKey) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read json_web_key urn string from json: %w", err)
	}

	parsed, err := ParseJsonWebKey(s)
	if err != nil {
		return fmt.Errorf("parse json_web_key urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *JsonWebKey) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into JsonWebKey", value)
	}

	parsed, err := ParseJsonWebKey(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u JsonWebKey) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u JsonWebKey) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal json_web_key urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *JsonWebKey) UnmarshalText(text []byte) error {
	parsed, err := ParseJsonWebKey(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal json_web_key urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *JsonWebKey) validate() error {
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
