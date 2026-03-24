package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type Deployment struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewDeployment(id uuid.UUID) Deployment {
	t := Deployment{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = t.validate()

	return t
}

func ParseDeployment(value string) (Deployment, error) {
	if value == "" {
		return Deployment{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return Deployment{}, fmt.Errorf("%w: expected two segments (deployment:<uuid>)", ErrInvalid)
	}

	if parts[0] != "deployment" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return Deployment{}, fmt.Errorf("%w: expected deployment urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return Deployment{}, fmt.Errorf("%w: invalid deployment uuid", ErrInvalid)
	}

	return NewDeployment(id), nil
}

func (u Deployment) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u Deployment) String() string {
	return "deployment" + delimiter + u.ID.String()
}

func (u Deployment) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("deployment urn to json: %w", err)
	}

	return b, nil
}

func (u *Deployment) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read deployment urn string from json: %w", err)
	}

	parsed, err := ParseDeployment(s)
	if err != nil {
		return fmt.Errorf("parse deployment urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *Deployment) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into Deployment", value)
	}

	parsed, err := ParseDeployment(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u Deployment) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u Deployment) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal deployment urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *Deployment) UnmarshalText(text []byte) error {
	parsed, err := ParseDeployment(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal deployment urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *Deployment) validate() error {
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
