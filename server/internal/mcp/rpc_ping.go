package mcp

import (
	"encoding/json"
)

func handlePing(id msgID) (json.RawMessage, error) {
	return json.Marshal(&result[struct{}]{
		ID:     id,
		Result: struct{}{},
	})
}
