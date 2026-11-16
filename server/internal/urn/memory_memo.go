package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// MemoryMemo is the URN for a team memory memo (the memory_memos table). It is
// used by the audit log to identify memos saved through remember.
type MemoryMemo struct {
	ID uuid.UUID
}

func NewMemoryMemo(id uuid.UUID) MemoryMemo {
	return MemoryMemo{ID: id}
}

func ParseMemoryMemo(value string) (MemoryMemo, error) {
	if value == "" {
		return MemoryMemo{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return MemoryMemo{}, fmt.Errorf("%w: expected two segments (memory_memo:<uuid>)", ErrInvalid)
	}

	if parts[0] != "memory_memo" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return MemoryMemo{}, fmt.Errorf("%w: expected memory memo urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return MemoryMemo{}, fmt.Errorf("%w: invalid memory memo uuid", ErrInvalid)
	}
	if id == uuid.Nil {
		return MemoryMemo{}, fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return NewMemoryMemo(id), nil
}

func (u MemoryMemo) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u MemoryMemo) String() string {
	return "memory_memo" + delimiter + u.ID.String()
}

func (u MemoryMemo) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("memory memo urn to json: %w", err)
	}

	return b, nil
}

func (u *MemoryMemo) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read memory memo urn string from json: %w", err)
	}

	parsed, err := ParseMemoryMemo(s)
	if err != nil {
		return fmt.Errorf("parse memory memo urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *MemoryMemo) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into MemoryMemo", value)
	}

	parsed, err := ParseMemoryMemo(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u MemoryMemo) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u MemoryMemo) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal memory memo urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *MemoryMemo) UnmarshalText(text []byte) error {
	parsed, err := ParseMemoryMemo(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal memory memo urn text: %w", err)
	}

	*u = parsed

	return nil
}

// validate is not cached: ID is exported, so a verdict could go stale.
func (u MemoryMemo) validate() error {
	if u.ID == uuid.Nil {
		return fmt.Errorf("%w: empty id", ErrInvalid)
	}

	return nil
}
