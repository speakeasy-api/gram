package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type ModelProviderKey struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewModelProviderKey(id uuid.UUID) ModelProviderKey {
	k := ModelProviderKey{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = k.validate()

	return k
}

func ParseModelProviderKey(value string) (ModelProviderKey, error) {
	if value == "" {
		return ModelProviderKey{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return ModelProviderKey{}, fmt.Errorf("%w: expected two segments (model_provider_key:<uuid>)", ErrInvalid)
	}

	if parts[0] != "model_provider_key" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return ModelProviderKey{}, fmt.Errorf("%w: expected model_provider_key urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return ModelProviderKey{}, fmt.Errorf("%w: invalid model_provider_key uuid", ErrInvalid)
	}

	return NewModelProviderKey(id), nil
}

func (u ModelProviderKey) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u ModelProviderKey) String() string {
	return "model_provider_key" + delimiter + u.ID.String()
}

func (u ModelProviderKey) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("model_provider_key urn to json: %w", err)
	}

	return b, nil
}

func (u *ModelProviderKey) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read model_provider_key urn string from json: %w", err)
	}

	parsed, err := ParseModelProviderKey(s)
	if err != nil {
		return fmt.Errorf("parse model_provider_key urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *ModelProviderKey) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into ModelProviderKey", value)
	}

	parsed, err := ParseModelProviderKey(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u ModelProviderKey) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u ModelProviderKey) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal model_provider_key urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *ModelProviderKey) UnmarshalText(text []byte) error {
	parsed, err := ParseModelProviderKey(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal model_provider_key urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *ModelProviderKey) validate() error {
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
