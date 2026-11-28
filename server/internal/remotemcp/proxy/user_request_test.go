package proxy

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseJSONRPCMessagesReturnsBufferedReadError(t *testing.T) {
	t.Parallel()

	readErr := errors.New("truncated request body")
	request := &UserRequest{UserHTTPRequest: &http.Request{
		Body: NewBufferedRequestBody([]byte(`{"jsonrpc":"2.0","method":"tools/call"}`), readErr),
	}}

	err := request.ParseJSONRPCMessages(1024)
	require.ErrorIs(t, err, readErr)
}
