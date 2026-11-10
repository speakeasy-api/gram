package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// WorkloadIssuer identifies a row in workload_issuers: an external issuer an
// organization trusts to vouch for its workloads.
//
// Distinct from SessionSubject's workload kind and from PrincipalTypeWorkload,
// which identify the machine itself. This names the signer, not the signed.
type WorkloadIssuer struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewWorkloadIssuer(id uuid.UUID) WorkloadIssuer {
	a := WorkloadIssuer{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseWorkloadIssuer(value string) (WorkloadIssuer, error) {
	if value == "" {
		return WorkloadIssuer{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return WorkloadIssuer{}, fmt.Errorf("%w: expected two segments (workload_issuer:<uuid>)", ErrInvalid)
	}

	if parts[0] != "workload_issuer" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return WorkloadIssuer{}, fmt.Errorf("%w: expected workload_issuer urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return WorkloadIssuer{}, fmt.Errorf("%w: invalid workload_issuer uuid", ErrInvalid)
	}

	return NewWorkloadIssuer(id), nil
}

func (u WorkloadIssuer) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u WorkloadIssuer) String() string {
	return "workload_issuer" + delimiter + u.ID.String()
}

func (u WorkloadIssuer) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("workload_issuer urn to json: %w", err)
	}

	return b, nil
}

func (u *WorkloadIssuer) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read workload_issuer urn string from json: %w", err)
	}

	parsed, err := ParseWorkloadIssuer(s)
	if err != nil {
		return fmt.Errorf("parse workload_issuer urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *WorkloadIssuer) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into WorkloadIssuer", value)
	}

	parsed, err := ParseWorkloadIssuer(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u WorkloadIssuer) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u WorkloadIssuer) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal workload_issuer urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *WorkloadIssuer) UnmarshalText(text []byte) error {
	parsed, err := ParseWorkloadIssuer(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal workload_issuer urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *WorkloadIssuer) validate() error {
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
