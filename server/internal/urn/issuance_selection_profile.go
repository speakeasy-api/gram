package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type IssuanceSelectionProfile struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewIssuanceSelectionProfile(id uuid.UUID) IssuanceSelectionProfile {
	a := IssuanceSelectionProfile{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseIssuanceSelectionProfile(value string) (IssuanceSelectionProfile, error) {
	if value == "" {
		return IssuanceSelectionProfile{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return IssuanceSelectionProfile{}, fmt.Errorf("%w: expected two segments (issuance_selection_profile:<uuid>)", ErrInvalid)
	}

	if parts[0] != "issuance_selection_profile" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return IssuanceSelectionProfile{}, fmt.Errorf("%w: expected issuance_selection_profile urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return IssuanceSelectionProfile{}, fmt.Errorf("%w: invalid issuance_selection_profile uuid", ErrInvalid)
	}

	a := NewIssuanceSelectionProfile(id)
	if a.err != nil {
		return IssuanceSelectionProfile{}, a.err
	}

	return a, nil
}

func (u IssuanceSelectionProfile) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u IssuanceSelectionProfile) String() string {
	return "issuance_selection_profile" + delimiter + u.ID.String()
}

func (u IssuanceSelectionProfile) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("issuance_selection_profile urn to json: %w", err)
	}

	return b, nil
}

func (u *IssuanceSelectionProfile) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read issuance_selection_profile urn string from json: %w", err)
	}

	parsed, err := ParseIssuanceSelectionProfile(s)
	if err != nil {
		return fmt.Errorf("parse issuance_selection_profile urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *IssuanceSelectionProfile) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into IssuanceSelectionProfile", value)
	}

	parsed, err := ParseIssuanceSelectionProfile(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u IssuanceSelectionProfile) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u IssuanceSelectionProfile) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal issuance_selection_profile urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *IssuanceSelectionProfile) UnmarshalText(text []byte) error {
	parsed, err := ParseIssuanceSelectionProfile(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal issuance_selection_profile urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *IssuanceSelectionProfile) validate() error {
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
