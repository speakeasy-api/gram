package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// SpendRule is a versioned URN for a spend control rule. The version segment
// pins the exact rule configuration that produced an event so historical
// events remain interpretable after the rule is edited.
type SpendRule struct {
	ID      uuid.UUID
	Version int64

	checked bool
	err     error
}

func NewSpendRule(id uuid.UUID, version int64) SpendRule {
	s := SpendRule{
		ID:      id,
		Version: version,
		checked: false,
		err:     nil,
	}

	_ = s.validate()

	return s
}

func ParseSpendRule(value string) (SpendRule, error) {
	if value == "" {
		return SpendRule{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 3)
	if len(parts) != 3 || parts[1] == "" || parts[2] == "" || strings.Contains(parts[2], delimiter) {
		return SpendRule{}, fmt.Errorf("%w: expected three segments (spend_rule:<uuid>:v<version>)", ErrInvalid)
	}

	if parts[0] != "spend_rule" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return SpendRule{}, fmt.Errorf("%w: expected spend_rule urn (got: %q)", ErrInvalid, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return SpendRule{}, fmt.Errorf("%w: invalid spend_rule uuid", ErrInvalid)
	}

	rawVersion, ok := strings.CutPrefix(parts[2], "v")
	if !ok || rawVersion == "" || len(parts[2]) > maxSegmentLength {
		return SpendRule{}, fmt.Errorf("%w: expected version segment (v<version>)", ErrInvalid)
	}

	version, err := strconv.ParseInt(rawVersion, 10, 64)
	if err != nil || version < 1 {
		return SpendRule{}, fmt.Errorf("%w: invalid spend_rule version", ErrInvalid)
	}

	return NewSpendRule(id, version), nil
}

func (u SpendRule) IsZero() bool {
	return u.ID == uuid.Nil && u.Version == 0
}

func (u SpendRule) String() string {
	return "spend_rule" + delimiter + u.ID.String() + delimiter + "v" + strconv.FormatInt(u.Version, 10)
}

func (u SpendRule) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("spend_rule urn to json: %w", err)
	}

	return b, nil
}

func (u *SpendRule) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read spend_rule urn string from json: %w", err)
	}

	parsed, err := ParseSpendRule(s)
	if err != nil {
		return fmt.Errorf("parse spend_rule urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *SpendRule) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into SpendRule", value)
	}

	parsed, err := ParseSpendRule(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u SpendRule) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u SpendRule) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal spend_rule urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *SpendRule) UnmarshalText(text []byte) error {
	parsed, err := ParseSpendRule(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal spend_rule urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *SpendRule) validate() error {
	if u.checked {
		return u.err
	}

	u.checked = true

	if u.ID == uuid.Nil {
		u.err = fmt.Errorf("%w: empty id", ErrInvalid)
		return u.err
	}

	if u.Version < 1 {
		u.err = fmt.Errorf("%w: version must be at least 1", ErrInvalid)
		return u.err
	}

	return nil
}
