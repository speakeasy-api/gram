package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type DirectoryRoleMapping struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewDirectoryRoleMapping(id uuid.UUID) DirectoryRoleMapping {
	a := DirectoryRoleMapping{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseDirectoryRoleMapping(value string) (DirectoryRoleMapping, error) {
	if value == "" {
		return DirectoryRoleMapping{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return DirectoryRoleMapping{}, fmt.Errorf("%w: expected two segments (directory_role_mapping:<uuid>)", ErrInvalid)
	}

	if parts[0] != "directory_role_mapping" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return DirectoryRoleMapping{}, fmt.Errorf("%w: expected directory_role_mapping urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return DirectoryRoleMapping{}, fmt.Errorf("%w: invalid directory_role_mapping uuid", ErrInvalid)
	}

	return NewDirectoryRoleMapping(id), nil
}

func (u DirectoryRoleMapping) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u DirectoryRoleMapping) String() string {
	return "directory_role_mapping" + delimiter + u.ID.String()
}

func (u DirectoryRoleMapping) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("directory_role_mapping urn to json: %w", err)
	}

	return b, nil
}

func (u *DirectoryRoleMapping) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read directory_role_mapping urn string from json: %w", err)
	}

	parsed, err := ParseDirectoryRoleMapping(s)
	if err != nil {
		return fmt.Errorf("parse directory_role_mapping urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *DirectoryRoleMapping) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into DirectoryRoleMapping", value)
	}

	parsed, err := ParseDirectoryRoleMapping(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u DirectoryRoleMapping) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u DirectoryRoleMapping) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal directory_role_mapping urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *DirectoryRoleMapping) UnmarshalText(text []byte) error {
	parsed, err := ParseDirectoryRoleMapping(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal directory_role_mapping urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *DirectoryRoleMapping) validate() error {
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
