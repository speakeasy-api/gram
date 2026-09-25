package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// WorkloadAdmission identifies a row in workload_identity_admissions: one
// subject an organization recognises, under one issuer, at one tier.
//
// The row is the grant of machine access, which is why it is audited under its
// own subject rather than folded into the issuer's.
type WorkloadAdmission struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewWorkloadAdmission(id uuid.UUID) WorkloadAdmission {
	a := WorkloadAdmission{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseWorkloadAdmission(value string) (WorkloadAdmission, error) {
	if value == "" {
		return WorkloadAdmission{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return WorkloadAdmission{}, fmt.Errorf("%w: expected two segments (workload_admission:<uuid>)", ErrInvalid)
	}

	if parts[0] != "workload_admission" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return WorkloadAdmission{}, fmt.Errorf("%w: expected workload_admission urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return WorkloadAdmission{}, fmt.Errorf("%w: invalid workload_admission uuid", ErrInvalid)
	}

	return NewWorkloadAdmission(id), nil
}

func (u WorkloadAdmission) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u WorkloadAdmission) String() string {
	return "workload_admission" + delimiter + u.ID.String()
}

func (u WorkloadAdmission) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("workload_admission urn to json: %w", err)
	}

	return b, nil
}

func (u *WorkloadAdmission) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read workload_admission urn string from json: %w", err)
	}

	parsed, err := ParseWorkloadAdmission(s)
	if err != nil {
		return fmt.Errorf("parse workload_admission urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *WorkloadAdmission) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into WorkloadAdmission", value)
	}

	parsed, err := ParseWorkloadAdmission(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u WorkloadAdmission) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u WorkloadAdmission) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal workload_admission urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *WorkloadAdmission) UnmarshalText(text []byte) error {
	parsed, err := ParseWorkloadAdmission(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal workload_admission urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *WorkloadAdmission) validate() error {
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
