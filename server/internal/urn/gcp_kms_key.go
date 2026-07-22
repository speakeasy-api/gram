package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// GcpKmsKey is a 2-segment URN identifying an external_keys row whose provider
// is gcp_kms. Format: "gcp_kms_key:<uuid>".
type GcpKmsKey struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewGcpKmsKey(id uuid.UUID) GcpKmsKey {
	a := GcpKmsKey{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseGcpKmsKey(value string) (GcpKmsKey, error) {
	if value == "" {
		return GcpKmsKey{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return GcpKmsKey{}, fmt.Errorf("%w: expected two segments (gcp_kms_key:<uuid>)", ErrInvalid)
	}

	if parts[0] != "gcp_kms_key" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return GcpKmsKey{}, fmt.Errorf("%w: expected gcp_kms_key urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return GcpKmsKey{}, fmt.Errorf("%w: invalid gcp_kms_key uuid", ErrInvalid)
	}

	return NewGcpKmsKey(id), nil
}

func (u GcpKmsKey) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u GcpKmsKey) String() string {
	return "gcp_kms_key" + delimiter + u.ID.String()
}

func (u GcpKmsKey) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("gcp_kms_key urn to json: %w", err)
	}

	return b, nil
}

func (u *GcpKmsKey) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read gcp_kms_key urn string from json: %w", err)
	}

	parsed, err := ParseGcpKmsKey(s)
	if err != nil {
		return fmt.Errorf("parse gcp_kms_key urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *GcpKmsKey) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into GcpKmsKey", value)
	}

	parsed, err := ParseGcpKmsKey(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u GcpKmsKey) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u GcpKmsKey) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal gcp_kms_key urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *GcpKmsKey) UnmarshalText(text []byte) error {
	parsed, err := ParseGcpKmsKey(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal gcp_kms_key urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *GcpKmsKey) validate() error {
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
