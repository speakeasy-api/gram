package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type DataExportRoute struct {
	ID uuid.UUID
}

func NewDataExportRoute(id uuid.UUID) DataExportRoute {
	return DataExportRoute{ID: id}
}

func ParseDataExportRoute(value string) (DataExportRoute, error) {
	if value == "" {
		return DataExportRoute{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return DataExportRoute{}, fmt.Errorf("%w: expected two segments (data_export_route:<uuid>)", ErrInvalid)
	}

	if parts[0] != "data_export_route" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return DataExportRoute{}, fmt.Errorf("%w: expected data_export_route urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return DataExportRoute{}, fmt.Errorf("%w: invalid data_export_route uuid", ErrInvalid)
	}

	return NewDataExportRoute(id), nil
}

func (u DataExportRoute) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u DataExportRoute) String() string {
	return "data_export_route" + delimiter + u.ID.String()
}

func (u DataExportRoute) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("data_export_route urn to json: %w", err)
	}

	return b, nil
}

func (u *DataExportRoute) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read data_export_route urn string from json: %w", err)
	}

	parsed, err := ParseDataExportRoute(s)
	if err != nil {
		return fmt.Errorf("parse data_export_route urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *DataExportRoute) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into DataExportRoute", value)
	}

	parsed, err := ParseDataExportRoute(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u DataExportRoute) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u DataExportRoute) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal data_export_route urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *DataExportRoute) UnmarshalText(text []byte) error {
	parsed, err := ParseDataExportRoute(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal data_export_route urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u DataExportRoute) validate() error {
	if u.ID == uuid.Nil {
		return fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return nil
}
