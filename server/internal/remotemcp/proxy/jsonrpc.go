package proxy

import (
	"fmt"
	"io"
	"reflect"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

// jsonrpcIDsEqual compares two jsonrpc.ID values by their underlying
// representation. The ID type aliases an SDK-internal struct whose fields
// are unexported, so direct comparison isn't possible; we compare via the
// exposed Raw() value. The SDK's MakeID normalizes numeric IDs to int64
// regardless of source (decoded JSON or explicitly constructed), so types
// are stable across both sides of the comparison.
func jsonrpcIDsEqual(a, b jsonrpc.ID) bool {
	return reflect.DeepEqual(a.Raw(), b.Raw())
}

// readJSONRPCBody reads up to maxBytes from r, returning the raw body bytes
// and the decoded JSON-RPC message. Empty bodies return (nil, nil, nil) so
// callers can preserve status-only responses without invoking interceptors.
// Bodies exceeding maxBytes return [ErrBodyTooLarge].
func readJSONRPCBody(r io.Reader, maxBytes int64) ([]byte, jsonrpc.Message, error) {
	body, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, nil, fmt.Errorf("read body: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return nil, nil, ErrBodyTooLarge
	}
	if len(body) == 0 {
		return nil, nil, nil
	}
	msg, err := jsonrpc.DecodeMessage(body)
	if err != nil {
		return body, nil, fmt.Errorf("decode jsonrpc message: %w", err)
	}
	return body, msg, nil
}
