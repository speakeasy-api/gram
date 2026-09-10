package mcp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type failingRequestBody struct {
	err error
}

func (b failingRequestBody) Read([]byte) (int, error) {
	return 0, b.err
}

func (failingRequestBody) Close() error {
	return nil
}

func TestPreparedMCPRequest_ZeroByteReadErrorIsNotEmpty(t *testing.T) {
	t.Parallel()

	readErr := errors.New("read failed")
	req := httptest.NewRequest(http.MethodPost, "/mcp/test", nil)
	req.Body = failingRequestBody{err: readErr}
	prepared := prepareMCPRequest(httptest.NewRecorder(), req, 1<<20, mcpversions.SupportedHostedToolset())

	require.False(t, prepared.empty())
	err := validateMCPRequestEnvelope(t.Context(), testenv.NewLogger(t), prepared, oops.CodeBadRequest, "body too large")
	require.Error(t, err)
	require.ErrorIs(t, err, readErr)

	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeBadRequest, shareable.Code)
}

func TestValidateMCPRequestEnvelope_MalformedArrayIsParseError(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/mcp/test", strings.NewReader("["))
	prepared := prepareMCPRequest(httptest.NewRecorder(), req, 1<<20, mcpversions.SupportedHostedToolset())

	err := validateMCPRequestEnvelope(t.Context(), testenv.NewLogger(t), prepared, oops.CodeBadRequest, "body too large")
	require.Error(t, err)

	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeParseError, shareable.Code)
}
