package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type OktaServerSuggestion struct {
	ID uuid.UUID
}

func NewOktaServerSuggestion(id uuid.UUID) OktaServerSuggestion {
	return OktaServerSuggestion{ID: id}
}

func ParseOktaServerSuggestion(value string) (OktaServerSuggestion, error) {
	if value == "" {
		return OktaServerSuggestion{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return OktaServerSuggestion{}, fmt.Errorf("%w: expected two segments (okta_server_suggestion:<uuid>)", ErrInvalid)
	}

	if parts[0] != "okta_server_suggestion" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return OktaServerSuggestion{}, fmt.Errorf("%w: expected okta_server_suggestion urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return OktaServerSuggestion{}, fmt.Errorf("%w: invalid okta_server_suggestion uuid", ErrInvalid)
	}
	if id == uuid.Nil {
		return OktaServerSuggestion{}, fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return NewOktaServerSuggestion(id), nil
}

func (u OktaServerSuggestion) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u OktaServerSuggestion) String() string {
	return "okta_server_suggestion" + delimiter + u.ID.String()
}

func (u OktaServerSuggestion) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("okta_server_suggestion urn to json: %w", err)
	}

	return b, nil
}

func (u *OktaServerSuggestion) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read okta_server_suggestion urn string from json: %w", err)
	}

	parsed, err := ParseOktaServerSuggestion(s)
	if err != nil {
		return fmt.Errorf("parse okta_server_suggestion urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *OktaServerSuggestion) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into OktaServerSuggestion", value)
	}

	parsed, err := ParseOktaServerSuggestion(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u OktaServerSuggestion) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u OktaServerSuggestion) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal okta_server_suggestion urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *OktaServerSuggestion) UnmarshalText(text []byte) error {
	parsed, err := ParseOktaServerSuggestion(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal okta_server_suggestion urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u OktaServerSuggestion) validate() error {
	if u.ID == uuid.Nil {
		return fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return nil
}
