package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type BillingMetadata struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewBillingMetadata(id uuid.UUID) BillingMetadata {
	b := BillingMetadata{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = b.validate()

	return b
}

func ParseBillingMetadata(value string) (BillingMetadata, error) {
	if value == "" {
		return BillingMetadata{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return BillingMetadata{}, fmt.Errorf("%w: expected two segments (billing-metadata:<uuid>)", ErrInvalid)
	}

	if parts[0] != "billing-metadata" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return BillingMetadata{}, fmt.Errorf("%w: expected billing-metadata urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return BillingMetadata{}, fmt.Errorf("%w: invalid billing-metadata uuid", ErrInvalid)
	}

	return NewBillingMetadata(id), nil
}

func (u BillingMetadata) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u BillingMetadata) String() string {
	return "billing-metadata" + delimiter + u.ID.String()
}

func (u BillingMetadata) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("billing-metadata urn to json: %w", err)
	}

	return b, nil
}

func (u *BillingMetadata) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read billing-metadata urn string from json: %w", err)
	}

	parsed, err := ParseBillingMetadata(s)
	if err != nil {
		return fmt.Errorf("parse billing-metadata urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *BillingMetadata) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into BillingMetadata", value)
	}

	parsed, err := ParseBillingMetadata(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u BillingMetadata) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u BillingMetadata) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal billing-metadata urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *BillingMetadata) UnmarshalText(text []byte) error {
	parsed, err := ParseBillingMetadata(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal billing-metadata urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *BillingMetadata) validate() error {
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
