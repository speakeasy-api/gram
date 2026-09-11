package mcpjsonrpc

import (
	"encoding/json"
	"fmt"
	"strconv"
)

const (
	idFormatNumber byte = 1
	idFormatString byte = 2
	idFormatNull   byte = 3
)

type ID struct {
	format byte // 1 for int64, 3 for null. any other non-zero value means string.
	Number int64
	String string
}

func NullID() ID {
	return ID{format: idFormatNull, Number: 0, String: ""}
}

func NumberID(value int64) ID {
	return ID{format: idFormatNumber, Number: value, String: ""}
}

func StringID(value string) ID {
	return ID{format: idFormatString, Number: 0, String: value}
}

func (id ID) IsSet() bool {
	return id.format != 0
}

// IsNull reports whether id serializes as JSON null, which happens both when
// the request carried no id at all — as a notification does — and when it
// carried an explicit null. Either way a response bearing this id answers no
// particular request, so callers deciding how to answer one treat the two the
// same.
func (id ID) IsNull() bool {
	return !id.IsSet() || id.format == idFormatNull
}

func (id ID) Value() string {
	switch id.format {
	case idFormatNumber:
		return strconv.FormatInt(id.Number, 10)
	case idFormatNull:
		return ""
	default:
		return id.String
	}
}

func (id ID) MarshalJSON() ([]byte, error) {
	if id.IsNull() {
		return []byte("null"), nil
	}

	var bs []byte
	var err error
	switch id.format {
	case idFormatNumber:
		bs, err = json.Marshal(id.Number)
	default:
		bs, err = json.Marshal(id.String)
	}
	if err != nil {
		return nil, fmt.Errorf("marshal json-rpc id: %w", err)
	}

	return bs, nil
}

func (id *ID) UnmarshalJSON(data []byte) error {
	var raw json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("unmarshal json-rpc id: %w", err)
	}

	if string(raw) == "null" {
		id.format = idFormatNull
		id.Number = 0
		id.String = ""
		return nil
	}

	var intID int64
	if err := json.Unmarshal(raw, &intID); err == nil {
		id.format = idFormatNumber
		id.Number = intID
		id.String = ""
		return nil
	}

	var strID string
	if err := json.Unmarshal(raw, &strID); err == nil {
		id.format = idFormatString
		id.Number = 0
		id.String = strID
		return nil
	}

	return fmt.Errorf("json-rpc id must be an integer, string, or null: %s", string(raw))
}
