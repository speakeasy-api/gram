package conv

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"log/slog"
)

type Secret struct{ value string }

func (s Secret) Reveal() string {
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
		*s = Secret{value: ""}
		return nil
	}

	val := Secret{value: string(text)}
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
	var val string
	if err := json.Unmarshal(data, &val); err != nil {
		return fmt.Errorf("unmarshal secret from json: %w", err)
	}

	*s = Secret{value: val}
	return nil
}

func (s *Secret) Scan(value any) error {
	if value == nil {
		return nil
	}

	var val string
	switch v := value.(type) {
	case string:
		val = v
	case []byte:
		val = string(v)
	default:
		return fmt.Errorf("cannot scan %T into Secret", value)
	}

	*s = Secret{value: val}

	return nil
}

func (s Secret) Value() (driver.Value, error) {
	return s.Reveal(), nil
}

func (s Secret) LogValue() slog.Value {
	return slog.StringValue(s.String())
}
