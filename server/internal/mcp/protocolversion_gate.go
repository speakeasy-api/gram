package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// preparedMCPRequest carries the single bounded body read and JSON decode
// across authentication and dispatch. It lets terminating surfaces validate
// protocol metadata before authentication without consuming the body twice.
type preparedMCPRequest struct {
	body            []byte
	bodyReadErr     error
	bodyDecodeErr   error
	request         rawRequest
	protocolVersion mcpversions.Resolution
}

func prepareMCPRequest(w http.ResponseWriter, r *http.Request, maxBodyBytes int64, supported []string) *preparedMCPRequest {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	body, bodyReadErr := io.ReadAll(r.Body)

	var req rawRequest
	var bodyDecodeErr error
	if bodyReadErr == nil {
		bodyDecodeErr = json.Unmarshal(body, &req)
		if bodyDecodeErr == nil {
			if rpcCtx, ok := contextvalues.GetRPCContext(r.Context()); ok && req.ID.IsSet() {
				rpcCtx.ID = req.ID
			}
		}
	}

	protocolVersion := mcpversions.Resolve(
		mcprequests.DeclaredProtocolVersion(r.Header.Get(mcpversions.HTTPHeader), req.Params),
		supported,
	)
	if rpcCtx, ok := contextvalues.GetRPCContext(r.Context()); ok {
		rpcCtx.ProtocolVersion = protocolVersion.InEffect
	}

	return &preparedMCPRequest{
		body:            body,
		bodyReadErr:     bodyReadErr,
		bodyDecodeErr:   bodyDecodeErr,
		request:         req,
		protocolVersion: protocolVersion,
	}
}

func (p *preparedMCPRequest) empty() bool {
	return len(p.body) == 0 && (p.bodyReadErr == nil || errors.Is(p.bodyReadErr, io.EOF))
}

// readyForProtocolVersionValidation limits the pre-authentication gate to
// complete JSON-RPC request envelopes. Authentication retains precedence for
// empty, unreadable, malformed, and otherwise invalid envelopes.
func (p *preparedMCPRequest) readyForProtocolVersionValidation() bool {
	return !p.empty() && p.bodyReadErr == nil && p.bodyDecodeErr == nil && p.request.JSONRPC == "2.0"
}

// validateMCPRequestEnvelope applies the shared transport and JSON-RPC shape
// checks needed before a declaration can be trusted as request metadata.
func validateMCPRequestEnvelope(ctx context.Context, logger *slog.Logger, p *preparedMCPRequest, bodyTooLargeCode oops.Code, bodyTooLargeMessage string) error {
	var maxBytesErr *http.MaxBytesError
	switch {
	case errors.As(p.bodyReadErr, &maxBytesErr):
		return oops.E(bodyTooLargeCode, p.bodyReadErr, "%s", bodyTooLargeMessage).LogError(ctx, logger)
	case p.bodyReadErr != nil:
		return oops.E(oops.CodeBadRequest, p.bodyReadErr, "failed to read request body").LogError(ctx, logger)
	case p.bodyDecodeErr != nil && !json.Valid(p.body):
		return oops.E(oops.CodeParseError, p.bodyDecodeErr, "failed to decode request body").LogError(ctx, logger)
	case len(p.body) > 0 && p.body[0] == '[':
		return oops.E(oops.CodeBadRequest, nil, "batch requests are not supported").LogError(ctx, logger)
	case p.bodyDecodeErr != nil:
		return oops.E(oops.CodeBadRequest, p.bodyDecodeErr, "failed to decode request body").LogError(ctx, logger)
	case p.request.JSONRPC != "2.0":
		return oops.E(oops.CodeBadRequest, errInvalidJSONRPCVersion, "unsupported JSON-RPC version").LogError(ctx, logger)
	default:
		return nil
	}
}

func validateSupportedProtocolVersion(req *rawRequest, resolution mcpversions.Resolution, supported []string) error {
	if req.Method == "initialize" || resolution.Declared == "" || slices.Contains(supported, resolution.Declared) {
		return nil
	}

	return unsupportedProtocolVersionError(req.ID, resolution.Declared, supported)
}

func (s *Service) prepareTerminatedMCPRequest(
	w http.ResponseWriter,
	r *http.Request,
	logger *slog.Logger,
	maxBodyBytes int64,
	supported []string,
	surface mcpmetrics.Surface,
) (*preparedMCPRequest, bool, error) {
	prepared := prepareMCPRequest(w, r, maxBodyBytes, supported)
	if !prepared.readyForProtocolVersionValidation() {
		return prepared, false, nil
	}

	handled, err := s.handleProtocolVersionValidation(
		r.Context(),
		logger,
		w,
		&prepared.request,
		prepared.protocolVersion,
		surface,
		validateSupportedProtocolVersion(&prepared.request, prepared.protocolVersion, supported),
	)
	if handled || err != nil {
		return nil, handled, err
	}

	return prepared, false, nil
}

func unsupportedProtocolVersionError(id mcpjsonrpc.ID, requested string, supported []string) *oops.MCPError {
	return &oops.MCPError{
		ID:      id,
		Code:    oops.MCPCodeUnsupportedProtocolVersion,
		Message: oops.MCPCodeUnsupportedProtocolVersion.Message(),
		Data: &oops.MCPErrorData{
			Code:      "",
			Supported: slices.Clone(supported),
			Requested: requested,
		},
	}
}

// handleProtocolVersionValidation writes a validation failure before
// authentication. Notifications are acknowledged without a JSON-RPC body and
// are not dispatched. Only genuine unsupported-version failures contribute to
// the rejection census; malformed and conflicting meta declarations retain
// their existing invalid-request behavior.
func (s *Service) handleProtocolVersionValidation(
	ctx context.Context,
	logger *slog.Logger,
	w http.ResponseWriter,
	req *rawRequest,
	resolution mcpversions.Resolution,
	surface mcpmetrics.Surface,
	validationErr error,
) (bool, error) {
	if validationErr == nil {
		return false, nil
	}

	var mcpErr *oops.MCPError
	if errors.As(validationErr, &mcpErr) && mcpErr.Code == oops.MCPCodeUnsupportedProtocolVersion {
		s.metrics.RecordMCPProtocolVersionRejected(ctx, resolution.Declared, req.Method, surface)
	}

	if !req.ID.IsSet() {
		return true, respondWithNoContent(true, w)
	}

	return true, writeMCPError(ctx, logger, w, req.ID, resolution.InEffect, validationErr)
}
