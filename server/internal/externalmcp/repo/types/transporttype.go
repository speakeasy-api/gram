package types

import (
	"database/sql/driver"
	"fmt"
)

type TransportType string

const (
	TransportTypeStreamableHTTP TransportType = "streamable-http"
	TransportTypeSSE            TransportType = "sse"
)

func (t TransportType) Value() (driver.Value, error) {
	return string(t), nil
}

func (t *TransportType) Scan(value any) error {
	if value == nil {
		*t = ""
		return nil
	}

	str, ok := value.(string)
	if !ok {
		return fmt.Errorf("failed to scan TransportType: %v", value)
	}

	*t = TransportType(str)
	return nil
}

func (t TransportType) Valid() bool {
	return t == TransportTypeStreamableHTTP || t == TransportTypeSSE
}

func (t TransportType) String() string {
	return string(t)
}
