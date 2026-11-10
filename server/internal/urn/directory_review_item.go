package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type DirectoryReviewItem struct {
	ID uuid.UUID

	checked bool
	err     error
}

func NewDirectoryReviewItem(id uuid.UUID) DirectoryReviewItem {
	a := DirectoryReviewItem{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseDirectoryReviewItem(value string) (DirectoryReviewItem, error) {
	if value == "" {
		return DirectoryReviewItem{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return DirectoryReviewItem{}, fmt.Errorf("%w: expected two segments (directory_review_item:<uuid>)", ErrInvalid)
	}

	if parts[0] != "directory_review_item" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return DirectoryReviewItem{}, fmt.Errorf("%w: expected directory_review_item urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return DirectoryReviewItem{}, fmt.Errorf("%w: invalid directory_review_item uuid", ErrInvalid)
	}
	if id == uuid.Nil {
		return DirectoryReviewItem{}, fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return NewDirectoryReviewItem(id), nil
}

func (u DirectoryReviewItem) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u DirectoryReviewItem) String() string {
	return "directory_review_item" + delimiter + u.ID.String()
}

func (u DirectoryReviewItem) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("directory_review_item urn to json: %w", err)
	}

	return b, nil
}

func (u *DirectoryReviewItem) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read directory_review_item urn string from json: %w", err)
	}

	parsed, err := ParseDirectoryReviewItem(s)
	if err != nil {
		return fmt.Errorf("parse directory_review_item urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *DirectoryReviewItem) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into DirectoryReviewItem", value)
	}

	parsed, err := ParseDirectoryReviewItem(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u DirectoryReviewItem) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u DirectoryReviewItem) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal directory_review_item urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *DirectoryReviewItem) UnmarshalText(text []byte) error {
	parsed, err := ParseDirectoryReviewItem(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal directory_review_item urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *DirectoryReviewItem) validate() error {
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
