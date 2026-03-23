package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type Asset struct {
	Kind AssetKind
	ID   uuid.UUID

	checked bool
	err     error
}

func NewAsset(kind AssetKind, id uuid.UUID) Asset {
	a := Asset{
		Kind:    kind,
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseAsset(value string) (Asset, error) {
	if value == "" {
		return Asset{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 3)
	if len(parts) != 3 || parts[1] == "" || parts[2] == "" || strings.Contains(parts[2], delimiter) {
		return Asset{}, fmt.Errorf("%w: expected three segments (assets:<kind>:<uuid>)", ErrInvalid)
	}

	if parts[0] != "assets" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return Asset{}, fmt.Errorf("%w: expected assets urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[2])
	if err != nil {
		return Asset{}, fmt.Errorf("%w: invalid asset uuid", ErrInvalid)
	}

	a := Asset{
		Kind:    AssetKind(parts[1]),
		ID:      id,
		checked: false,
		err:     nil,
	}

	if err := a.validate(); err != nil {
		return Asset{}, err
	}

	return a, nil
}

func (u Asset) IsZero() bool {
	return u.Kind == "" && u.ID == uuid.Nil
}

func (u Asset) String() string {
	return "assets" + delimiter + string(u.Kind) + delimiter + u.ID.String()
}

func (u Asset) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("asset urn to json: %w", err)
	}

	return b, nil
}

func (u *Asset) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read asset urn string from json: %w", err)
	}

	parsed, err := ParseAsset(s)
	if err != nil {
		return fmt.Errorf("parse asset urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *Asset) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into Asset", value)
	}

	parsed, err := ParseAsset(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u Asset) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u Asset) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal asset urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *Asset) UnmarshalText(text []byte) error {
	parsed, err := ParseAsset(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal asset urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *Asset) validate() error {
	if u.checked {
		return u.err
	}

	u.checked = true

	if _, ok := assetKinds[u.Kind]; !ok {
		u.err = fmt.Errorf("%w: unknown asset kind: %q", ErrInvalid, u.Kind)
		return u.err
	}

	if u.ID == uuid.Nil {
		u.err = fmt.Errorf("%w: empty id", ErrInvalid)
		return u.err
	}

	if u.ID.Version() != 7 {
		u.err = fmt.Errorf("%w: asset id must be uuid v7", ErrInvalid)
		return u.err
	}

	return nil
}
