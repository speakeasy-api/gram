package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type InferenceProfile struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewInferenceProfile(id uuid.UUID) InferenceProfile {
	a := InferenceProfile{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseInferenceProfile(value string) (InferenceProfile, error) {
	if value == "" {
		return InferenceProfile{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return InferenceProfile{}, fmt.Errorf("%w: expected two segments (inference_profile:<uuid>)", ErrInvalid)
	}

	if parts[0] != "inference_profile" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return InferenceProfile{}, fmt.Errorf("%w: expected inference_profile urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return InferenceProfile{}, fmt.Errorf("%w: invalid inference_profile uuid", ErrInvalid)
	}

	return NewInferenceProfile(id), nil
}

func (u InferenceProfile) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u InferenceProfile) String() string {
	return "inference_profile" + delimiter + u.ID.String()
}

func (u InferenceProfile) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("inference_profile urn to json: %w", err)
	}

	return b, nil
}

func (u *InferenceProfile) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read inference_profile urn string from json: %w", err)
	}

	parsed, err := ParseInferenceProfile(s)
	if err != nil {
		return fmt.Errorf("parse inference_profile urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *InferenceProfile) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into InferenceProfile", value)
	}

	parsed, err := ParseInferenceProfile(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u InferenceProfile) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u InferenceProfile) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal inference_profile urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *InferenceProfile) UnmarshalText(text []byte) error {
	parsed, err := ParseInferenceProfile(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal inference_profile urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *InferenceProfile) validate() error {
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
