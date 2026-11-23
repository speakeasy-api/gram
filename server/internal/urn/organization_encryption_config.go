package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// OrganizationEncryptionConfig is a 2-segment URN identifying an organization_encryption_configs row.
// Format: "organization_encryption_config:<uuid>".
type OrganizationEncryptionConfig struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewOrganizationEncryptionConfig(id uuid.UUID) OrganizationEncryptionConfig {
	a := OrganizationEncryptionConfig{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseOrganizationEncryptionConfig(value string) (OrganizationEncryptionConfig, error) {
	if value == "" {
		return OrganizationEncryptionConfig{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return OrganizationEncryptionConfig{}, fmt.Errorf("%w: expected two segments (organization_encryption_config:<uuid>)", ErrInvalid)
	}

	if parts[0] != "organization_encryption_config" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return OrganizationEncryptionConfig{}, fmt.Errorf("%w: expected organization_encryption_config urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return OrganizationEncryptionConfig{}, fmt.Errorf("%w: invalid organization_encryption_config uuid", ErrInvalid)
	}

	if id == uuid.Nil {
		return OrganizationEncryptionConfig{}, fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return NewOrganizationEncryptionConfig(id), nil
}

func (u OrganizationEncryptionConfig) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u OrganizationEncryptionConfig) String() string {
	return "organization_encryption_config" + delimiter + u.ID.String()
}

func (u OrganizationEncryptionConfig) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("organization_encryption_config urn to json: %w", err)
	}

	return b, nil
}

func (u *OrganizationEncryptionConfig) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read organization_encryption_config urn string from json: %w", err)
	}

	parsed, err := ParseOrganizationEncryptionConfig(s)
	if err != nil {
		return fmt.Errorf("parse organization_encryption_config urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *OrganizationEncryptionConfig) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into OrganizationEncryptionConfig", value)
	}

	parsed, err := ParseOrganizationEncryptionConfig(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u OrganizationEncryptionConfig) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u OrganizationEncryptionConfig) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal organization_encryption_config urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *OrganizationEncryptionConfig) UnmarshalText(text []byte) error {
	parsed, err := ParseOrganizationEncryptionConfig(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal organization_encryption_config urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *OrganizationEncryptionConfig) validate() error {
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
