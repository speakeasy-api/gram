package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// InternalSessionIssuer is a 2-segment URN identifying a internal_session_issuers row.
// Format: "internal_session_issuer:<uuid>".
type InternalSessionIssuer struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewInternalSessionIssuer(id uuid.UUID) InternalSessionIssuer {
	a := InternalSessionIssuer{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseInternalSessionIssuer(value string) (InternalSessionIssuer, error) {
	if value == "" {
		return InternalSessionIssuer{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return InternalSessionIssuer{}, fmt.Errorf("%w: expected two segments (internal_session_issuer:<uuid>)", ErrInvalid)
	}

	if parts[0] != "internal_session_issuer" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return InternalSessionIssuer{}, fmt.Errorf("%w: expected internal_session_issuer urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return InternalSessionIssuer{}, fmt.Errorf("%w: invalid internal_session_issuer uuid", ErrInvalid)
	}

	return NewInternalSessionIssuer(id), nil
}

func (u InternalSessionIssuer) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u InternalSessionIssuer) String() string {
	return "internal_session_issuer" + delimiter + u.ID.String()
}

func (u InternalSessionIssuer) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("internal_session_issuer urn to json: %w", err)
	}

	return b, nil
}

func (u *InternalSessionIssuer) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read internal_session_issuer urn string from json: %w", err)
	}

	parsed, err := ParseInternalSessionIssuer(s)
	if err != nil {
		return fmt.Errorf("parse internal_session_issuer urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *InternalSessionIssuer) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into InternalSessionIssuer", value)
	}

	parsed, err := ParseInternalSessionIssuer(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u InternalSessionIssuer) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u InternalSessionIssuer) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal internal_session_issuer urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *InternalSessionIssuer) UnmarshalText(text []byte) error {
	parsed, err := ParseInternalSessionIssuer(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal internal_session_issuer urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *InternalSessionIssuer) validate() error {
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
