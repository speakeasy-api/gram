package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	negotiableInitialize := initializeNegotiable(req, resolution)
	if !negotiableInitialize {
		// Outside handshake negotiation, declarations must agree before
		// the resolved revision controls method availability.
		if metaVersion := mcprequests.DeclaredProtocolVersion("", req.Params); metaVersion != "" && metaVersion != resolution.Declared {
			return conflictingProtocolVersionError(req.ID, resolution.Declared, metaVersion)
		}
	}
	if negotiableInitialize || resolution.Declared == "" || slices.Contains(supported, resolution.Declared) {
		return nil
	}

	return unsupportedProtocolVersionError(req.ID, resolution.Declared, supported)
}

// conflictingProtocolVersionError reports an MCP-Protocol-Version header that
// does not mirror the request's `_meta` declaration: the MCP 2026-07-28
// HeaderMismatch (-32020). Only that revision defines the `_meta`
// declaration, and with the two disagreeing the request names no revision of
// its own, so the response follows [mcpversions.Latest]. Both values must be
// sanitized; raw hostile bytes are never echoed to the client.
func conflictingProtocolVersionError(id mcpjsonrpc.ID, headerVersion, metaVersion string) *declarationError {
	return &declarationError{
		revision: mcpversions.Latest(),
		err: &oops.MCPError{
			ID:      id,
			Code:    oops.MCPCodeHeaderMismatch,
			Message: fmt.Sprintf("conflicting protocol version declarations: MCP-Protocol-Version header %q does not match the request _meta declaration %q", headerVersion, metaVersion),
			Data:    nil,
		},
	}
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

	validationErr := validateSupportedProtocolVersion(&prepared.request, prepared.protocolVersion, supported)
	if validationErr == nil {
		validationErr = validateRequestMetadata(r.Header, &prepared.request, prepared.protocolVersion)
	}
	handled, err := s.handleProtocolVersionValidation(
		r,
		logger,
		w,
		&prepared.request,
		prepared.protocolVersion,
		surface,
		validationErr,
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
			Code:                 "",
			Supported:            slices.Clone(supported),
			Requested:            requested,
			RequiredCapabilities: nil,
		},
	}
}

// handleProtocolVersionValidation writes a request validation failure before
// authentication. Notifications are acknowledged without a JSON-RPC body and
// are not dispatched. Every failure, notifications included, is counted on
// mcp.request.rejected by reason; unsupported-version failures are also
// counted on the protocol-version rejection census. The response is encoded
// under the revision in effect, unless the failure is a [declarationError]
// naming the revision that governs it.
func (s *Service) handleProtocolVersionValidation(
	r *http.Request,
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
	ctx := r.Context()

	if mcpErr, ok := errors.AsType[*oops.MCPError](validationErr); ok {
		if mcpErr.Code == oops.MCPCodeUnsupportedProtocolVersion {
			s.metrics.RecordMCPProtocolVersionRejected(ctx, resolution.Declared, req.Method, surface)
		}
		if reason, ok := requestRejectionReason(mcpErr.Code); ok {
			s.metrics.RecordMCPRequestValidationRejected(ctx, reason, rejectedRequestMCPURL(r), surface)
		}
	}

	if !req.ID.IsSet() {
		return true, respondWithNoContent(true, w)
	}

	revision := resolution.InEffect
	if declErr, ok := errors.AsType[*declarationError](validationErr); ok {
		revision = declErr.revision
	}

	return true, writeMCPError(ctx, logger, w, req.ID, revision, validationErr)
}

// requestRejectionReason maps a request validation failure to its bounded
// mcp.request.rejected reason.
func requestRejectionReason(code oops.MCPCode) (mcpmetrics.RequestRejectionReason, bool) {
	switch code {
	case oops.MCPCodeHeaderMismatch:
		return mcpmetrics.RequestRejectionReasonHeaderMismatch, true
	case oops.MCPCodeInvalidParams:
		return mcpmetrics.RequestRejectionReasonMetadataInvalid, true
	case oops.MCPCodeUnsupportedProtocolVersion:
		return mcpmetrics.RequestRejectionReasonProtocolVersionUnsupported, true
	default:
		return "", false
	}
}

// rejectedRequestMCPURL is the gram.mcp.url value for a request rejected by
// validation: the request context's host and the routed path, matching the
// issuer gate's value, without the query string a caller could vary to mint
// metric series. Validation runs only after routing resolved an existing
// endpoint, so the path is bounded by real endpoints.
func rejectedRequestMCPURL(r *http.Request) string {
	host := ""
	if requestContext, _ := contextvalues.GetRequestContext(r.Context()); requestContext != nil {
		host = requestContext.Host
	}
	return host + r.URL.Path
}
