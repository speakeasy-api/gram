package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// AwsIamCredential is a 2-segment URN identifying an external_credentials row
// whose provider is aws_iam. Format: "aws_iam_credential:<uuid>".
type AwsIamCredential struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewAwsIamCredential(id uuid.UUID) AwsIamCredential {
	a := AwsIamCredential{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseAwsIamCredential(value string) (AwsIamCredential, error) {
	if value == "" {
		return AwsIamCredential{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return AwsIamCredential{}, fmt.Errorf("%w: expected two segments (aws_iam_credential:<uuid>)", ErrInvalid)
	}

	if parts[0] != "aws_iam_credential" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return AwsIamCredential{}, fmt.Errorf("%w: expected aws_iam_credential urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return AwsIamCredential{}, fmt.Errorf("%w: invalid aws_iam_credential uuid", ErrInvalid)
	}

	return NewAwsIamCredential(id), nil
}

func (u AwsIamCredential) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u AwsIamCredential) String() string {
	return "aws_iam_credential" + delimiter + u.ID.String()
}

func (u AwsIamCredential) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("aws_iam_credential urn to json: %w", err)
	}

	return b, nil
}

func (u *AwsIamCredential) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read aws_iam_credential urn string from json: %w", err)
	}

	parsed, err := ParseAwsIamCredential(s)
	if err != nil {
		return fmt.Errorf("parse aws_iam_credential urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *AwsIamCredential) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into AwsIamCredential", value)
	}

	parsed, err := ParseAwsIamCredential(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u AwsIamCredential) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u AwsIamCredential) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal aws_iam_credential urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *AwsIamCredential) UnmarshalText(text []byte) error {
	parsed, err := ParseAwsIamCredential(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal aws_iam_credential urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *AwsIamCredential) validate() error {
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
