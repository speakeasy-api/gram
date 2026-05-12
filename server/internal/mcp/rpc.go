package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
)

const (
	MetaGramKind     = "gram.ai/kind"
	MetaGramMimeType = "getgram.ai/mime-type"
)

var (
	errInvalidJSONRPCVersion = errors.New("invalid json-rpc version")
)

type resultEnvelope[T any] struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      mcpjsonrpc.ID `json:"id"`
	Result  T             `json:"result,omitempty,omitzero"`
}

type result[T any] struct {
	ID     mcpjsonrpc.ID `json:"id"`
	Result T             `json:"result,omitempty,omitzero"`
}

func (m result[T]) MarshalJSON() ([]byte, error) {
	bs, err := json.Marshal(resultEnvelope[T]{
		JSONRPC: "2.0",
		ID:      m.ID,
		Result:  m.Result,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}

	return bs, nil
}

func (m *result[T]) UnmarshalJSON(data []byte) error {
	var envelope resultEnvelope[T]
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("unmarshal result envelope: %w", err)
	}

	if envelope.JSONRPC != "2.0" {
		return fmt.Errorf("%w: %s", errInvalidJSONRPCVersion, envelope.JSONRPC)
	}

	*m = result[T]{
		ID:     envelope.ID,
		Result: envelope.Result,
	}

	return nil
}

type rawRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      mcpjsonrpc.ID   `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func respondWithNoContent(ack bool, w http.ResponseWriter) error {
	acks := strconv.FormatBool(ack)
	w.Header().Set("Noop", acks)
	w.WriteHeader(http.StatusAccepted)
	return nil
}
