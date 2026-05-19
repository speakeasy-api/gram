package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type OrganizationInvitation struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewOrganizationInvitation(id uuid.UUID) OrganizationInvitation {
	u := OrganizationInvitation{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = u.validate()

	return u
}

func ParseOrganizationInvitation(value string) (OrganizationInvitation, error) {
	if value == "" {
		return OrganizationInvitation{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return OrganizationInvitation{}, fmt.Errorf("%w: expected two segments (organization-invitation:<uuid>)", ErrInvalid)
	}

	if parts[0] != "organization-invitation" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return OrganizationInvitation{}, fmt.Errorf("%w: expected organization-invitation urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return OrganizationInvitation{}, fmt.Errorf("%w: invalid organization-invitation uuid", ErrInvalid)
	}

	return NewOrganizationInvitation(id), nil
}

func (u OrganizationInvitation) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u OrganizationInvitation) String() string {
	return "organization-invitation" + delimiter + u.ID.String()
}

func (u OrganizationInvitation) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("organization-invitation urn to json: %w", err)
	}

	return b, nil
}

func (u *OrganizationInvitation) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read organization-invitation urn string from json: %w", err)
	}

	parsed, err := ParseOrganizationInvitation(s)
	if err != nil {
		return fmt.Errorf("parse organization-invitation urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *OrganizationInvitation) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into OrganizationInvitation", value)
	}

	parsed, err := ParseOrganizationInvitation(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u OrganizationInvitation) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u OrganizationInvitation) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal organization-invitation urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *OrganizationInvitation) UnmarshalText(text []byte) error {
	parsed, err := ParseOrganizationInvitation(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal organization-invitation urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *OrganizationInvitation) validate() error {
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
