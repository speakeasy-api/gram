package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// OrganizationEncryptionTransition is a 2-segment URN identifying an organization_encryption_transitions row.
// Format: "organization_encryption_transition:<uuid>".
type OrganizationEncryptionTransition struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewOrganizationEncryptionTransition(id uuid.UUID) OrganizationEncryptionTransition {
	a := OrganizationEncryptionTransition{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseOrganizationEncryptionTransition(value string) (OrganizationEncryptionTransition, error) {
	if value == "" {
		return OrganizationEncryptionTransition{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return OrganizationEncryptionTransition{}, fmt.Errorf("%w: expected two segments (organization_encryption_transition:<uuid>)", ErrInvalid)
	}

	if parts[0] != "organization_encryption_transition" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return OrganizationEncryptionTransition{}, fmt.Errorf("%w: expected organization_encryption_transition urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return OrganizationEncryptionTransition{}, fmt.Errorf("%w: invalid organization_encryption_transition uuid", ErrInvalid)
	}

	if id == uuid.Nil {
		return OrganizationEncryptionTransition{}, fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return NewOrganizationEncryptionTransition(id), nil
}

func (u OrganizationEncryptionTransition) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u OrganizationEncryptionTransition) String() string {
	return "organization_encryption_transition" + delimiter + u.ID.String()
}

func (u OrganizationEncryptionTransition) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("organization_encryption_transition urn to json: %w", err)
	}

	return b, nil
}

func (u *OrganizationEncryptionTransition) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read organization_encryption_transition urn string from json: %w", err)
	}

	parsed, err := ParseOrganizationEncryptionTransition(s)
	if err != nil {
		return fmt.Errorf("parse organization_encryption_transition urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *OrganizationEncryptionTransition) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into OrganizationEncryptionTransition", value)
	}

	parsed, err := ParseOrganizationEncryptionTransition(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u OrganizationEncryptionTransition) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u OrganizationEncryptionTransition) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal organization_encryption_transition urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *OrganizationEncryptionTransition) UnmarshalText(text []byte) error {
	parsed, err := ParseOrganizationEncryptionTransition(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal organization_encryption_transition urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *OrganizationEncryptionTransition) validate() error {
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
