package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type InferenceProfileRule struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewInferenceProfileRule(id uuid.UUID) InferenceProfileRule {
	a := InferenceProfileRule{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseInferenceProfileRule(value string) (InferenceProfileRule, error) {
	if value == "" {
		return InferenceProfileRule{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return InferenceProfileRule{}, fmt.Errorf("%w: expected two segments (inference_profile_rule:<uuid>)", ErrInvalid)
	}

	if parts[0] != "inference_profile_rule" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return InferenceProfileRule{}, fmt.Errorf("%w: expected inference_profile_rule urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return InferenceProfileRule{}, fmt.Errorf("%w: invalid inference_profile_rule uuid", ErrInvalid)
	}

	return NewInferenceProfileRule(id), nil
}

func (u InferenceProfileRule) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u InferenceProfileRule) String() string {
	return "inference_profile_rule" + delimiter + u.ID.String()
}

func (u InferenceProfileRule) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("inference_profile_rule urn to json: %w", err)
	}

	return b, nil
}

func (u *InferenceProfileRule) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read inference_profile_rule urn string from json: %w", err)
	}

	parsed, err := ParseInferenceProfileRule(s)
	if err != nil {
		return fmt.Errorf("parse inference_profile_rule urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *InferenceProfileRule) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into InferenceProfileRule", value)
	}

	parsed, err := ParseInferenceProfileRule(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u InferenceProfileRule) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u InferenceProfileRule) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal inference_profile_rule urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *InferenceProfileRule) UnmarshalText(text []byte) error {
	parsed, err := ParseInferenceProfileRule(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal inference_profile_rule urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *InferenceProfileRule) validate() error {
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
