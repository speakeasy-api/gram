package conv

import (
	"database/sql"
	"database/sql/driver"
	"encoding"
	"encoding/json"
	"fmt"
	"log/slog"
)

type Secret struct{ value []byte }

var _ interface {
	fmt.Stringer

	sql.Scanner
	driver.Valuer

	json.Marshaler
	json.Unmarshaler

	encoding.TextMarshaler
	encoding.TextUnmarshaler

	slog.LogValuer
} = (*Secret)(nil)

var _ interface {
	fmt.Stringer

	driver.Valuer

	json.Marshaler

	encoding.TextMarshaler

	slog.LogValuer
} = Secret{value: []byte{}}

func NewSecret(value []byte) Secret {
	return Secret{value: value}
}

func (s Secret) Reveal() []byte {
	return s.value
}

func (s Secret) String() string {
	return "****"
}

func (s Secret) MarshalText() ([]byte, error) {
	return []byte(s.String()), nil
}

func (s *Secret) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		*s = NewSecret([]byte{})
		return nil
	}

	val := NewSecret(text)
	*s = val
	return nil
}

func (s Secret) MarshalJSON() ([]byte, error) {
	bs, err := json.Marshal(s.String())
	if err != nil {
		return nil, fmt.Errorf("marshal secret to json: %w", err)
	}

	return bs, nil
}

func (s *Secret) UnmarshalJSON(data []byte) error {
	if len(data) == 0 {
		*s = NewSecret([]byte{})
		return nil
	}

	var val []byte
	if err := json.Unmarshal(data, &val); err != nil {
		return fmt.Errorf("unmarshal secret from json: %w", err)
	}

	*s = NewSecret(val)
	return nil
}

func (s *Secret) Scan(value any) error {
	if value == nil {
		return nil
	}

	var val []byte
	switch v := value.(type) {
	case string:
		val = []byte(v)
	case []byte:
		val = v
	default:
		return fmt.Errorf("cannot scan %T into Secret", value)
	}

	*s = NewSecret(val)

	return nil
}

func (s Secret) Value() (driver.Value, error) {
	return s.Reveal(), nil
}

func (s Secret) LogValue() slog.Value {
	return slog.StringValue(s.String())
}
