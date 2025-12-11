package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	MetaGramKind     = "gram.ai/kind"
	MetaGramMimeType = "getgram.ai/mime-type"
)

type errorCode int

const (
	methodNotAllowed errorCode = -32000
	parseError       errorCode = -32700
	invalidRequest   errorCode = -32600
	methodNotFound   errorCode = -32601
	invalidParams    errorCode = -32602
	internalError    errorCode = -32603
)

func (e errorCode) UserMessage() string {
	switch e {
	case methodNotAllowed:
		return "method not allowed"
	case parseError:
		return "invalid json was received by the server"
	case invalidRequest:
		return "json sent is not a valid request object"
	case methodNotFound:
		return "method does not exist or is not available"
	case invalidParams:
		return "invalid method parameters"
	case internalError:
		return "internal json-rpc error"
	default:
		return "an unexpected error occurred"
	}
}

func (e errorCode) String() string {
	return fmt.Sprintf("%d", e)
}

var (
	errInvalidJSONRPCVersion = errors.New("invalid json-rpc version")
)

type msgID struct {
	format byte // 1 for int64. any other values means string.
	Number int64
	String string
}

func (m msgID) Value() string {
	switch m.format {
	case 1:
		return fmt.Sprintf("%d", m.Number)
	default:
		return m.String
	}
}

func (m msgID) MarshalJSON() ([]byte, error) {
	var bs []byte
	var err error

	switch m.format {
	case 1:
		bs, err = json.Marshal(m.Number)
	default:
		bs, err = json.Marshal(m.String)
	}
	if err != nil {
		return nil, fmt.Errorf("marshal message id: %w", err)
	}

	return bs, nil
}

func (m *msgID) UnmarshalJSON(data []byte) error {
	var intid int64
	var str string

	if err := json.Unmarshal(data, &intid); err == nil {
		m.format = 1
		m.Number = intid
		return nil
	}

	if err := json.Unmarshal(data, &str); err == nil {
		m.format = 2
		m.String = str
		return nil
	}

	return fmt.Errorf("message id must be an integer or string: %s", string(data))
}

type resultEnvelope[T any] struct {
	JSONRPC string `json:"jsonrpc"`
	ID      msgID  `json:"id"`
	Result  T      `json:"result,omitempty,omitzero"`
}

type result[T any] struct {
	ID     msgID `json:"id"`
	Result T     `json:"result,omitempty,omitzero"`
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
	ID      msgID           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type batchedRawRequest []*rawRequest

func (b *batchedRawRequest) UnmarshalJSON(data []byte) error {
	var many []*rawRequest
	var err error
	if manyErr := json.Unmarshal(data, &many); manyErr == nil {
		*b = many
		return nil
	} else {
		err = manyErr
	}

	var one rawRequest
	if oneErr := json.Unmarshal(data, &one); oneErr == nil {
		*b = batchedRawRequest{&one}
		return nil
	} else {
		return err
	}
}

type rpcError struct {
	ID      msgID
	Code    errorCode
	Message string
	Data    any
}

func NewErrorFromCause(id msgID, source error) *rpcError {
	var rpce *rpcError
	var oopse *oops.ShareableError

	switch {
	case errors.As(source, &rpce):
		return rpce
	case errors.As(source, &oopse):
		var code errorCode
		switch oopse.Code {
		case oops.CodeBadRequest:
			code = parseError
		case oops.CodeUnauthorized, oops.CodeForbidden, oops.CodeConflict, oops.CodeUnsupportedMedia, oops.CodeNotFound:
			code = invalidRequest
		case oops.CodeInvalid:
			code = invalidParams
		case oops.CodeUnexpected:
			code = internalError
		default:
			code = internalError
		}

		return &rpcError{ID: id, Code: code, Message: oopse.Error(), Data: nil}
	default:
		return &rpcError{
			ID:      id,
			Code:    internalError,
			Message: internalError.UserMessage(),
			Data:    nil,
		}
	}
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("%d: %s", e.Code, e.Message)
}

func (e *rpcError) MarshalJSON() ([]byte, error) {
	if e == nil {
		return nil, nil
	}

	msg := e.Message
	if msg == "" {
		msg = e.Code.UserMessage()
	}

	payload := map[string]any{
		"jsonrpc": "2.0",
		"error": map[string]any{
			"code":    e.Code,
			"message": msg,
			"data":    e.Data,
		},
	}

	if (e.ID.format == 1 && e.ID.Number != 0) || (e.ID.format != 1 && e.ID.String != "") {
		payload["id"] = e.ID
	}

	bs, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal jsonrpc error: %w", err)
	}

	return bs, nil
}

func respondWithNoContent(ack bool, w http.ResponseWriter) error {
	acks := strconv.FormatBool(ack)
	w.Header().Set("Noop", acks)
	w.WriteHeader(http.StatusAccepted)
	return nil
}
