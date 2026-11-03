package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// CatalogPromotion identifies a durable reviewed catalogue promotion.
type CatalogPromotion struct {
	// ID is the durable publication receipt identifier.
	ID uuid.UUID
}

func NewCatalogPromotion(id uuid.UUID) CatalogPromotion {
	return CatalogPromotion{ID: id}
}

func ParseCatalogPromotion(value string) (CatalogPromotion, error) {
	if value == "" {
		return CatalogPromotion{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return CatalogPromotion{}, fmt.Errorf("%w: expected two segments (catalog-promotion:<uuid>)", ErrInvalid)
	}

	if parts[0] != "catalog-promotion" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return CatalogPromotion{}, fmt.Errorf("%w: expected catalog-promotion urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil || id == uuid.Nil {
		return CatalogPromotion{}, fmt.Errorf("%w: invalid catalog-promotion uuid", ErrInvalid)
	}

	return NewCatalogPromotion(id), nil
}

func (u CatalogPromotion) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u CatalogPromotion) String() string {
	return "catalog-promotion" + delimiter + u.ID.String()
}

func (u CatalogPromotion) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("catalog-promotion urn to json: %w", err)
	}

	return b, nil
}

func (u *CatalogPromotion) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read catalog-promotion urn string from json: %w", err)
	}

	parsed, err := ParseCatalogPromotion(s)
	if err != nil {
		return fmt.Errorf("parse catalog-promotion urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *CatalogPromotion) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into CatalogPromotion", value)
	}

	parsed, err := ParseCatalogPromotion(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u CatalogPromotion) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u CatalogPromotion) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal catalog-promotion urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *CatalogPromotion) UnmarshalText(text []byte) error {
	parsed, err := ParseCatalogPromotion(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal catalog-promotion urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u CatalogPromotion) validate() error {
	if u.ID == uuid.Nil {
		return fmt.Errorf("%w: empty id", ErrInvalid)
	}
	return nil
}
