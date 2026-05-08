package proxy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

// UserRequest captures an inbound MCP client HTTP request along with any
// JSON-RPC messages decoded from its body. Interceptors mutate this value
// before the request is forwarded to the remote MCP server.
type UserRequest struct {
	UserHTTPRequest *http.Request
	JSONRPCMessages []jsonrpc.Message

	// body caches the raw request body so it can be parsed and later replayed
	// to the upstream forwarder without re-reading the original stream.
	body []byte
}

// BodyReader returns an io.Reader over the raw user request body so callers
// can forward the bytes upstream after ParseJSONRPCMessages has consumed the
// original stream.
func (r *UserRequest) BodyReader() io.Reader {
	return bytes.NewReader(r.body)
}

// ParseJSONRPCMessages reads the request body and decodes it into
// JSONRPCMessages. The raw body is retained so BodyReader can reproduce it for
// forwarding after interceptors have run. MCP Streamable HTTP POST bodies
// carry a single JSON-RPC request, response, or notification, but the field is
// a slice to leave room for future batch handling.
//
// maxBytes caps the in-memory allocation during read; [ErrBodyTooLarge] is
// returned if the client sends more than that. The same limit is applied to
// user requests and remote responses so proxy memory use stays bounded on
// both sides. Streamed responses are not routed through this function and
// are not subject to this cap — see [Proxy.MaxBufferedBodyBytes].
func (r *UserRequest) ParseJSONRPCMessages(maxBytes int64) error {
	if r.UserHTTPRequest == nil || r.UserHTTPRequest.Body == nil {
		return nil
	}

	// Read up to maxBytes+1 so a fully-filled cap is distinguishable from an
	// oversized body.
	body, err := io.ReadAll(io.LimitReader(r.UserHTTPRequest.Body, maxBytes+1))
	if err != nil {
		return fmt.Errorf("read user request body: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return ErrBodyTooLarge
	}
	r.body = body

	if len(body) == 0 {
		return nil
	}

	msg, err := jsonrpc.DecodeMessage(body)
	if err != nil {
		return fmt.Errorf("decode jsonrpc message: %w", err)
	}
	r.JSONRPCMessages = []jsonrpc.Message{msg}

	return nil
}
