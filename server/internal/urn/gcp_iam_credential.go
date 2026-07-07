package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// GcpIamCredential is a 2-segment URN identifying an external_credentials row
// whose provider is gcp_iam. Format: "gcp_iam_credential:<uuid>".
type GcpIamCredential struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewGcpIamCredential(id uuid.UUID) GcpIamCredential {
	a := GcpIamCredential{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseGcpIamCredential(value string) (GcpIamCredential, error) {
	if value == "" {
		return GcpIamCredential{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return GcpIamCredential{}, fmt.Errorf("%w: expected two segments (gcp_iam_credential:<uuid>)", ErrInvalid)
	}

	if parts[0] != "gcp_iam_credential" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return GcpIamCredential{}, fmt.Errorf("%w: expected gcp_iam_credential urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return GcpIamCredential{}, fmt.Errorf("%w: invalid gcp_iam_credential uuid", ErrInvalid)
	}

	return NewGcpIamCredential(id), nil
}

func (u GcpIamCredential) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u GcpIamCredential) String() string {
	return "gcp_iam_credential" + delimiter + u.ID.String()
}

func (u GcpIamCredential) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("gcp_iam_credential urn to json: %w", err)
	}

	return b, nil
}

func (u *GcpIamCredential) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read gcp_iam_credential urn string from json: %w", err)
	}

	parsed, err := ParseGcpIamCredential(s)
	if err != nil {
		return fmt.Errorf("parse gcp_iam_credential urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *GcpIamCredential) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into GcpIamCredential", value)
	}

	parsed, err := ParseGcpIamCredential(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u GcpIamCredential) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u GcpIamCredential) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal gcp_iam_credential urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *GcpIamCredential) UnmarshalText(text []byte) error {
	parsed, err := ParseGcpIamCredential(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal gcp_iam_credential urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *GcpIamCredential) validate() error {
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
