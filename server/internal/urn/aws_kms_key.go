package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// AwsKmsKey is a 2-segment URN identifying an external_keys row whose provider
// is aws_kms. Format: "aws_kms_key:<uuid>".
type AwsKmsKey struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewAwsKmsKey(id uuid.UUID) AwsKmsKey {
	a := AwsKmsKey{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseAwsKmsKey(value string) (AwsKmsKey, error) {
	if value == "" {
		return AwsKmsKey{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return AwsKmsKey{}, fmt.Errorf("%w: expected two segments (aws_kms_key:<uuid>)", ErrInvalid)
	}

	if parts[0] != "aws_kms_key" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return AwsKmsKey{}, fmt.Errorf("%w: expected aws_kms_key urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return AwsKmsKey{}, fmt.Errorf("%w: invalid aws_kms_key uuid", ErrInvalid)
	}

	return NewAwsKmsKey(id), nil
}

func (u AwsKmsKey) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u AwsKmsKey) String() string {
	return "aws_kms_key" + delimiter + u.ID.String()
}

func (u AwsKmsKey) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("aws_kms_key urn to json: %w", err)
	}

	return b, nil
}

func (u *AwsKmsKey) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read aws_kms_key urn string from json: %w", err)
	}

	parsed, err := ParseAwsKmsKey(s)
	if err != nil {
		return fmt.Errorf("parse aws_kms_key urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *AwsKmsKey) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into AwsKmsKey", value)
	}

	parsed, err := ParseAwsKmsKey(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u AwsKmsKey) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u AwsKmsKey) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal aws_kms_key urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *AwsKmsKey) UnmarshalText(text []byte) error {
	parsed, err := ParseAwsKmsKey(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal aws_kms_key urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *AwsKmsKey) validate() error {
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
