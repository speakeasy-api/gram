package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type AccessRequest struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewAccessRequest(id uuid.UUID) AccessRequest {
	a := AccessRequest{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseAccessRequest(value string) (AccessRequest, error) {
	if value == "" {
		return AccessRequest{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return AccessRequest{}, fmt.Errorf("%w: expected two segments (access_request:<uuid>)", ErrInvalid)
	}

	if parts[0] != "access_request" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return AccessRequest{}, fmt.Errorf("%w: expected access_request urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return AccessRequest{}, fmt.Errorf("%w: invalid access_request uuid", ErrInvalid)
	}
	if id == uuid.Nil {
		return AccessRequest{}, fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return NewAccessRequest(id), nil
}

func (u AccessRequest) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u AccessRequest) String() string {
	return "access_request" + delimiter + u.ID.String()
}

func (u AccessRequest) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("access_request urn to json: %w", err)
	}

	return b, nil
}

func (u *AccessRequest) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read access_request urn string from json: %w", err)
	}

	parsed, err := ParseAccessRequest(s)
	if err != nil {
		return fmt.Errorf("parse access_request urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *AccessRequest) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into AccessRequest", value)
	}

	parsed, err := ParseAccessRequest(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u AccessRequest) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u AccessRequest) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal access_request urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *AccessRequest) UnmarshalText(text []byte) error {
	parsed, err := ParseAccessRequest(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal access_request urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *AccessRequest) validate() error {
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
