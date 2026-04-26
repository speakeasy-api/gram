package aiinsights

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/insights"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// scheme builds a minimal goa APIKeyScheme for the named scheme. auth.Auth
// dispatches on scheme.Name only for all schemes except api-key (which also
// reads RequiredScopes).
func scheme(name string) *security.APIKeyScheme {
	return &security.APIKeyScheme{Name: name}
}

// JSON-RPC 2.0 framing. The wire format mirrors server/internal/mcp/rpc.go —
// we cannot literally import those types because they're unexported, but the
// format is stable and this surface is deliberately small so drift is
// unlikely. If the wider mcp package exports its primitives in future we
// should switch to them.

const jsonrpcVersion = "2.0"

// JSON-RPC 2.0 error codes. These match the values in
// server/internal/mcp/rpc.go to keep clients uniform.
const (
	errCodeParseError     = -32700
	errCodeInvalidRequest = -32600
	errCodeMethodNotFound = -32601
	errCodeInvalidParams  = -32602
	errCodeInternalError  = -32603
)

// rpcMsgID supports both integer and string JSON-RPC ids (same shape as the
// mcp package's msgID).
type rpcMsgID struct {
	isNum  bool
	number int64
	str    string
}

func (m rpcMsgID) MarshalJSON() ([]byte, error) {
	if m.isNum {
		return json.Marshal(m.number)
	}
	// empty string → null per JSON-RPC 2.0 for notifications
	if m.str == "" {
		return []byte("null"), nil
	}
	return json.Marshal(m.str)
}

func (m *rpcMsgID) UnmarshalJSON(data []byte) error {
	var n int64
	if err := json.Unmarshal(data, &n); err == nil {
		m.isNum = true
		m.number = n
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		m.isNum = false
		m.str = s
		return nil
	}
	return fmt.Errorf("message id must be int or string: %s", string(data))
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      rpcMsgID        `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResultEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      rpcMsgID        `json:"id"`
	Result  json.RawMessage `json:"result"`
}

type rpcErrorBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type rpcErrorEnvelope struct {
	JSONRPC string       `json:"jsonrpc"`
	ID      rpcMsgID     `json:"id"`
	Error   rpcErrorBody `json:"error"`
}

// mcpContent is a single MCP text-content chunk returned inside a tools/call
// result.
type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolsCallResult struct {
	Content []mcpContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

type toolsListResult struct {
	Tools []toolListEntry `json:"tools"`
}

type toolListEntry struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

type initializeResult struct {
	ProtocolVersion string                     `json:"protocolVersion"`
	Capabilities    map[string]json.RawMessage `json:"capabilities"`
	ServerInfo      serverInfo                 `json:"serverInfo"`
	Instructions    string                     `json:"instructions,omitempty"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

const serverInstructions = `This is Gram's built-in AI Insights MCP server.

Use these tools to propose improvements to tools and toolsets, and to keep workspace memory across chat sessions. Investigation protocol:
  1. Form a single hypothesis.
  2. Gather evidence with your read-only tools.
  3. Record what you learned via insights_record_finding so you don't lose state between tool calls.
  4. If evidence points to a tool or toolset fix, call insights_propose_variation or insights_propose_toolset_change with a clear 'reasoning' field.

You cannot apply, roll back, or dismiss proposals — those are human-only actions. Propose, and let the user review.`

// Server is the HTTP handler for /mcp/ai-insights. It dispatches JSON-RPC 2.0
// requests to the six ai-insights MCP tools, which in turn call the
// insights.Service.
type Server struct {
	logger     *slog.Logger
	auth       *auth.Auth
	insights   *insights.Service
	tools      []Tool
	toolByName map[string]Tool
}

// New constructs the handler.
func New(logger *slog.Logger, authHelper *auth.Auth, insightsSvc *insights.Service) *Server {
	logger = logger.With(attr.SlogComponent("aiinsights-mcp"))
	tools := Tools()
	byName := make(map[string]Tool, len(tools))
	for _, t := range tools {
		byName[t.Name] = t
	}
	return &Server{
		logger:     logger,
		auth:       authHelper,
		insights:   insightsSvc,
		tools:      tools,
		toolByName: byName,
	}
}

// Attach registers POST /mcp/ai-insights on the given mux.
func Attach(mux goahttp.Muxer, s *Server) {
	o11y.AttachHandler(mux, http.MethodPost, "/mcp/ai-insights", oops.ErrHandle(s.logger, s.ServeHTTP).ServeHTTP)
	o11y.AttachHandler(mux, http.MethodGet, "/mcp/ai-insights", oops.ErrHandle(s.logger, s.serveGet).ServeHTTP)
}

// serveGet replies 405 for GET probes (e.g. SSE-capability checks). No auth
// is performed for this method — matches the customer MCP surface in
// server/internal/mcp/impl.go: HandleGetServer. Returning 403 here would
// confuse MCP clients that probe with GET to detect SSE support.
func (s *Server) serveGet(w http.ResponseWriter, _ *http.Request) error {
	body, err := json.Marshal(rpcErrorEnvelope{
		JSONRPC: jsonrpcVersion,
		ID:      rpcMsgID{},
		Error: rpcErrorBody{
			Code:    errCodeMethodNotFound,
			Message: "This MCP server uses POST-based Streamable HTTP transport. This GET request is a normal compatibility probe by the MCP client and can be safely ignored. The client will automatically use POST for actual communication.",
		},
	})
	if err != nil {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return nil
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusMethodNotAllowed)
	_, _ = w.Write(body)
	return nil
}

// ServeHTTP handles a single JSON-RPC 2.0 request posted to the server.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return s.writeError(w, rpcMsgID{}, errCodeParseError, "failed to read request body")
	}
	defer func() { _ = r.Body.Close() }()

	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return s.writeError(w, rpcMsgID{}, errCodeParseError, "invalid json-rpc request")
	}
	if req.JSONRPC != jsonrpcVersion {
		return s.writeError(w, req.ID, errCodeInvalidRequest, "expected jsonrpc: 2.0")
	}

	// Authenticate on every call. initialize + tools/list + tools/call all
	// require an authenticated principal; there is no unauthenticated
	// capability on this server.
	authCtx, err := s.authenticate(ctx, r)
	if err != nil {
		return s.writeErrorFromCause(w, req.ID, err)
	}

	switch req.Method {
	case "initialize":
		return s.handleInitialize(authCtx, w, &req)
	case "notifications/initialized", "notifications/cancelled":
		// No-content ack.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		return nil
	case "ping":
		return s.writeResult(w, req.ID, json.RawMessage("{}"))
	case "tools/list":
		return s.handleToolsList(w, &req)
	case "tools/call":
		return s.handleToolsCall(authCtx, w, &req)
	default:
		return s.writeError(w, req.ID, errCodeMethodNotFound, fmt.Sprintf("method not found: %s", req.Method))
	}
}

// authenticate resolves the caller to an auth context using the same schemes
// that /rpc/* uses: Gram-Session header (or gram_session cookie) or Gram-Key
// API key. A Gram-Project header is also required so the insights service
// has a project_id to scope against (matching the behavior of /rpc/insights.*).
func (s *Server) authenticate(ctx context.Context, r *http.Request) (context.Context, error) {
	// Read the session token (header takes precedence over cookie).
	if sessionTok := r.Header.Get(constants.SessionHeader); sessionTok != "" {
		newCtx, err := s.auth.Authorize(ctx, sessionTok, scheme(constants.SessionSecurityScheme))
		if err != nil {
			return ctx, err
		}
		ctx = newCtx
	} else if c, cerr := r.Cookie(constants.SessionCookie); cerr == nil && c.Value != "" {
		newCtx, err := s.auth.Authorize(ctx, c.Value, scheme(constants.SessionSecurityScheme))
		if err != nil {
			return ctx, err
		}
		ctx = newCtx
	} else if keyTok := r.Header.Get(constants.APIKeyHeader); keyTok != "" {
		newCtx, err := s.auth.Authorize(ctx, keyTok, scheme(constants.KeySecurityScheme))
		if err != nil {
			return ctx, err
		}
		ctx = newCtx
	} else {
		return ctx, oops.C(oops.CodeUnauthorized)
	}

	// Attach project from Gram-Project header (same scheme /rpc/insights.* uses).
	projectSlug := r.Header.Get(constants.ProjectHeader)
	newCtx, err := s.auth.Authorize(ctx, projectSlug, scheme(constants.ProjectSlugSecuritySchema))
	if err != nil {
		return ctx, err
	}

	// Sanity: require a project is now attached.
	if authCtx, ok := contextvalues.GetAuthContext(newCtx); !ok || authCtx == nil || authCtx.ProjectID == nil {
		return newCtx, oops.C(oops.CodeUnauthorized)
	}

	return newCtx, nil
}

func (s *Server) handleInitialize(_ context.Context, w http.ResponseWriter, req *rpcRequest) error {
	body := initializeResult{
		ProtocolVersion: "2025-03-26",
		Capabilities: map[string]json.RawMessage{
			"tools": json.RawMessage("{}"),
		},
		ServerInfo: serverInfo{
			Name:    "Gram AI Insights",
			Version: "0.1.0",
		},
		Instructions: serverInstructions,
	}
	bs, err := json.Marshal(body)
	if err != nil {
		return s.writeErrorFromCause(w, req.ID, err)
	}
	return s.writeResult(w, req.ID, bs)
}

func (s *Server) handleToolsList(w http.ResponseWriter, req *rpcRequest) error {
	entries := make([]toolListEntry, 0, len(s.tools))
	for _, t := range s.tools {
		entries = append(entries, toolListEntry{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}
	bs, err := json.Marshal(toolsListResult{Tools: entries})
	if err != nil {
		return s.writeErrorFromCause(w, req.ID, err)
	}
	return s.writeResult(w, req.ID, bs)
}

type toolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (s *Server) handleToolsCall(ctx context.Context, w http.ResponseWriter, req *rpcRequest) error {
	var params toolsCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return s.writeError(w, req.ID, errCodeInvalidParams, "failed to parse tools/call params")
	}
	if params.Name == "" {
		return s.writeError(w, req.ID, errCodeInvalidParams, "tool name is required")
	}

	tool, ok := s.toolByName[params.Name]
	if !ok {
		return s.writeError(w, req.ID, errCodeMethodNotFound, fmt.Sprintf("unknown tool: %s", params.Name))
	}

	args := params.Arguments
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}

	dispatchResult, err := tool.Dispatch(ctx, s.insights, args)
	if err != nil {
		// Prefer returning the error as an isError=true tool result rather
		// than a JSON-RPC error. Many MCP clients treat protocol errors as
		// fatal to the whole session; tool-level errors are recoverable.
		return s.writeToolError(w, req.ID, err)
	}

	wrapped := toolsCallResult{
		Content: []mcpContent{
			{Type: "text", Text: string(dispatchResult)},
		},
	}
	bs, mErr := json.Marshal(wrapped)
	if mErr != nil {
		return s.writeErrorFromCause(w, req.ID, mErr)
	}
	return s.writeResult(w, req.ID, bs)
}

// ---- Wire helpers ----

func (s *Server) writeResult(w http.ResponseWriter, id rpcMsgID, result json.RawMessage) error {
	env := rpcResultEnvelope{JSONRPC: jsonrpcVersion, ID: id, Result: result}
	bs, err := json.Marshal(env)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "serialize jsonrpc result")
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, werr := w.Write(bs)
	if werr != nil {
		return oops.E(oops.CodeUnexpected, werr, "write jsonrpc result")
	}
	return nil
}

func (s *Server) writeError(w http.ResponseWriter, id rpcMsgID, code int, message string) error {
	env := rpcErrorEnvelope{JSONRPC: jsonrpcVersion, ID: id, Error: rpcErrorBody{Code: code, Message: message}}
	bs, err := json.Marshal(env)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "serialize jsonrpc error")
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, werr := w.Write(bs)
	if werr != nil {
		return oops.E(oops.CodeUnexpected, werr, "write jsonrpc error")
	}
	return nil
}

// writeErrorFromCause maps an insights/oops error to the appropriate JSON-RPC
// error code. Mirrors NewErrorFromCause in server/internal/mcp/rpc.go.
func (s *Server) writeErrorFromCause(w http.ResponseWriter, id rpcMsgID, cause error) error {
	code, msg := mapError(cause)
	return s.writeError(w, id, code, msg)
}

// writeToolError formats an error as a successful tools/call result with
// isError=true (per MCP spec) rather than a JSON-RPC transport error.
func (s *Server) writeToolError(w http.ResponseWriter, id rpcMsgID, cause error) error {
	_, msg := mapError(cause)
	wrapped := toolsCallResult{
		Content: []mcpContent{{Type: "text", Text: msg}},
		IsError: true,
	}
	bs, err := json.Marshal(wrapped)
	if err != nil {
		return s.writeErrorFromCause(w, id, err)
	}
	return s.writeResult(w, id, bs)
}

func mapError(cause error) (int, string) {
	var oopse *oops.ShareableError
	if errors.As(cause, &oopse) {
		switch oopse.Code {
		case oops.CodeBadRequest:
			return errCodeParseError, oopse.Error()
		case oops.CodeUnauthorized, oops.CodeForbidden, oops.CodeConflict, oops.CodeUnsupportedMedia, oops.CodeNotFound:
			return errCodeInvalidRequest, oopse.Error()
		case oops.CodeInvalid:
			return errCodeInvalidParams, oopse.Error()
		default:
			return errCodeInternalError, oopse.Error()
		}
	}
	return errCodeInternalError, cause.Error()
}
