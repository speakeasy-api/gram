package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/speakeasy-api/gram/functions/internal/attr"
)

// maxRequestBodyBytes bounds a tool call or resource request body. Hosted MCP
// endpoints already reject requests over 1 MiB, so 4 MiB leaves room for the
// function's environment and callers outside MCP while keeping a single
// request from holding an unbounded buffer.
const maxRequestBodyBytes = 4 << 20 // 4 MiB

// decodeRequestBody decodes a JSON request body of at most maxRequestBodyBytes
// into v. On failure it writes the error response and returns false: 413 when
// the body is over the limit, 400 when it is malformed.
func (s *Service) decodeRequestBody(w http.ResponseWriter, r *http.Request, v any) bool {
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)).Decode(v)
	if err == nil {
		return true
	}

	status := http.StatusBadRequest
	if _, tooLarge := errors.AsType[*http.MaxBytesError](err); tooLarge {
		status = http.StatusRequestEntityTooLarge
	}

	s.logger.ErrorContext(r.Context(), "failed to decode request body", attr.SlogError(err))
	http.Error(w, fmt.Sprintf("decode request body: %s", err.Error()), status)
	return false
}
