package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type ExploreSavedQuery struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewExploreSavedQuery(id uuid.UUID) ExploreSavedQuery {
	query := ExploreSavedQuery{
		ID:      id,
		checked: false,
		err:     nil,
	}
	_ = query.validate()
	return query
}

func ParseExploreSavedQuery(value string) (ExploreSavedQuery, error) {
	if value == "" {
		return ExploreSavedQuery{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return ExploreSavedQuery{}, fmt.Errorf("%w: expected two segments (explore_saved_query:<uuid>)", ErrInvalid)
	}
	if parts[0] != "explore_saved_query" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return ExploreSavedQuery{}, fmt.Errorf("%w: expected explore_saved_query urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return ExploreSavedQuery{}, fmt.Errorf("%w: invalid explore_saved_query uuid", ErrInvalid)
	}
	return NewExploreSavedQuery(id), nil
}

func (u ExploreSavedQuery) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u ExploreSavedQuery) String() string {
	return "explore_saved_query" + delimiter + u.ID.String()
}

func (u ExploreSavedQuery) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("explore_saved_query urn to json: %w", err)
	}
	return data, nil
}

func (u *ExploreSavedQuery) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("read explore_saved_query urn string from json: %w", err)
	}
	parsed, err := ParseExploreSavedQuery(value)
	if err != nil {
		return fmt.Errorf("parse explore_saved_query urn json string: %w", err)
	}
	*u = parsed
	return nil
}

func (u *ExploreSavedQuery) Scan(value any) error {
	if value == nil {
		return nil
	}

	var text string
	switch typed := value.(type) {
	case string:
		text = typed
	case []byte:
		text = string(typed)
	default:
		return fmt.Errorf("cannot scan %T into ExploreSavedQuery", value)
	}

	parsed, err := ParseExploreSavedQuery(text)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}
	*u = parsed
	return nil
}

func (u ExploreSavedQuery) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}
	return u.String(), nil
}

func (u ExploreSavedQuery) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal explore_saved_query urn text: %w", err)
	}
	return []byte(u.String()), nil
}

func (u *ExploreSavedQuery) UnmarshalText(text []byte) error {
	parsed, err := ParseExploreSavedQuery(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal explore_saved_query urn text: %w", err)
	}
	*u = parsed
	return nil
}

func (u *ExploreSavedQuery) validate() error {
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
