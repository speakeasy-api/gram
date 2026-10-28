package approval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/risk/approval/repo"
)

const (
	// MCPPath is where the Approvals MCP server is mounted.
	MCPPath = "/approvals-mcp"

	// RequestPath is the endpoint an agent posts its approval request to,
	// with the request token as the bearer.
	RequestPath = "/approvals/request"

	// WatchPath is the endpoint the watcher polls with a request token.
	WatchPath = "/approvals/watch"

	// BuiltinBackend is the mcp_servers.builtin_backend value of a project's
	// approvals server, which the gateway serves with this runtime.
	BuiltinBackend = "approvals"

	// MCPServerName is the server name agents see.
	MCPServerName = "speakeasy-approvals"

	// watchIntervalSeconds is how often the watcher polls. Three seconds
	// feels immediate to a person who just clicked Approve and stays well
	// under the per-token rate limit.
	watchIntervalSeconds = 3

	// requestMaxBodyBytes bounds an approval request body: a reason of at most
	// 500 characters and an expiry.
	requestMaxBodyBytes = 16 << 10 // 16 KiB

	// mcpMaxBodyBytes bounds one MCP request. request_approval carries a token,
	// a sentence and two small options; 64 KiB leaves room for protocol framing.
	mcpMaxBodyBytes = 64 << 10 // 64 KiB
)

// Authenticator resolves the caller of an Approvals MCP request into an
// authenticated context, using the same credentials as gated MCP servers.
type Authenticator func(w http.ResponseWriter, r *http.Request) (context.Context, error)

// Runtime serves the Approvals MCP server and the request and watch
// endpoints.
type Runtime struct {
	service        *Service
	authenticate   Authenticator
	watchLimiter   *ratelimit.Limiter
	requestLimiter *ratelimit.Limiter
	server         *mcp.Server
	tools          []*mcp.Tool
}

var (
	// watchRate bounds polling per token. The watcher polls every 3 seconds
	// (20 a minute); 60 a minute leaves room for restarts and a second
	// watcher.
	watchRate = ratelimit.PerMinute(60)

	// requestRate bounds approval requests per token. Repeating a request is
	// harmless, but nobody needs to send one more than a few times.
	requestRate = ratelimit.PerMinute(20)
)

// NewRuntime builds the Approvals MCP server. A nil store disables rate
// limiting.
func NewRuntime(service *Service, authenticate Authenticator, store ratelimit.Store) *Runtime {
	var watchLimiter, requestLimiter *ratelimit.Limiter
	if store != nil {
		watchLimiter = ratelimit.New(store, "approval-watch", watchRate)
		requestLimiter = ratelimit.New(store, "approval-request", requestRate)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: MCPServerName, Title: "Speakeasy Approvals", Description: "", Version: "1.0.0", WebsiteURL: "", Icons: nil}, &mcp.ServerOptions{
		Instructions:                "Use request_approval when a tool call was blocked with a request token (apr_...). Pass the token and a one-sentence reason, then start the watcher exactly as its result says: with the Monitor tool in Claude Code, or in a background terminal elsewhere while you keep the turn open. Retry the blocked call with the same arguments once the watcher prints APPROVED.",
		Logger:                      nil,
		InitializedHandler:          nil,
		PageSize:                    0,
		RootsListChangedHandler:     nil,
		ProgressNotificationHandler: nil,
		CompletionHandler:           nil,
		KeepAlive:                   0,
		KeepAliveFailureThreshold:   0,
		SubscribeHandler:            nil,
		UnsubscribeHandler:          nil,
		Capabilities:                nil,
		HasPrompts:                  false,
		HasResources:                false,
		HasTools:                    true,
		SchemaCache:                 nil,
		GetSessionID:                nil,
	})
	tool := &mcp.Tool{
		Name:         "request_approval",
		Title:        "Request approval for a blocked tool call",
		Description:  "Ask a person to approve a tool call that a Speakeasy approval policy blocked. Pass the request token from the block message and one sentence explaining why the call is needed. Returns the steps and the watcher script to wait for the decision; follow them exactly.",
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: new(false), IdempotentHint: true, OpenWorldHint: new(false), Title: ""},
		Meta:         nil,
		InputSchema:  nil,
		OutputSchema: nil,
		Icons:        nil,
	}
	r := &Runtime{service: service, authenticate: authenticate, watchLimiter: watchLimiter, requestLimiter: requestLimiter, server: server, tools: []*mcp.Tool{tool}}
	mcp.AddTool(server, tool, r.requestApproval)
	return r
}

// Tools lists the Approvals MCP server's tools.
func (r *Runtime) Tools() []*mcp.Tool {
	return r.tools
}

// requestApprovalInput is the request_approval tool input.
type requestApprovalInput struct {
	// Token is the request token from the block message.
	Token string `json:"token" jsonschema:"The request token from the blocked call's message, starting with apr_."`

	// Reason is shown to approvers.
	Reason string `json:"reason" jsonschema:"One sentence telling the approver why this call is needed."`

	// Scope is what to approve.
	Scope string `json:"scope,omitempty" jsonschema:"What to approve. Only exact_once is supported: one retry of the exact blocked call."`

	// ExpiresIn is how long to wait for a decision, in seconds.
	ExpiresIn int `json:"expires_in,omitempty" jsonschema:"Seconds to wait for a decision. Defaults to 1800; between 60 and 86400."`
}

// requestApproval returns text only, with no output schema or structured
// content. Clients that get structured content show it to the model in place of
// the text (Claude Code does), and the text is where the watcher steps are.
func (r *Runtime) requestApproval(ctx context.Context, _ *mcp.CallToolRequest, input requestApprovalInput) (*mcp.CallToolResult, any, error) {
	identity, _ := mcpidentity.FromContext(ctx)
	requesterURN, _, ok := Requester(identity)
	authCtx, hasAuth := contextvalues.GetAuthContext(ctx)
	if !ok || !hasAuth || authCtx == nil {
		return nil, nil, errors.New("this credential cannot request approval; call with an agent key or sign in")
	}
	// Check the bounds in seconds first, so a huge value cannot wrap around
	// when it becomes a Duration.
	if input.ExpiresIn != 0 && (input.ExpiresIn < int(minRequestTTL/time.Second) || input.ExpiresIn > int(maxRequestTTL/time.Second)) {
		return nil, nil, fmt.Errorf("%w: expires_in must be between %d and %d seconds", ErrInvalidRequest, int(minRequestTTL/time.Second), int(maxRequestTTL/time.Second))
	}
	row, status, err := r.service.Request(ctx, RequestInput{
		Token:          strings.TrimSpace(input.Token),
		RequesterURN:   requesterURN,
		OrganizationID: authCtx.ActiveOrganizationID,
		Reason:         input.Reason,
		Scope:          input.Scope,
		ExpiresIn:      time.Duration(input.ExpiresIn) * time.Second,
	})
	switch {
	case errors.Is(err, ErrNotFound):
		return nil, nil, errors.New("no approval request matches this token for your credential; use the token from the block message")
	case errors.Is(err, ErrInvalidRequest):
		return nil, nil, err
	case err != nil:
		r.service.logger.ErrorContext(ctx, "request approval", attr.SlogError(err))
		return nil, nil, errors.New("the approval request could not be recorded; try again")
	}

	return &mcp.CallToolResult{
		Meta:              nil,
		Content:           []mcp.Content{&mcp.TextContent{Text: r.requestResultText(row, status), Meta: nil, Annotations: nil}},
		StructuredContent: nil,
		IsError:           false,
		InputRequests:     nil,
		RequestState:      "",
	}, nil, nil
}

func (r *Runtime) requestResultText(row repo.RiskPolicyApproval, status Status) string {
	switch status {
	case StatusApproved:
		return "Already approved. Retry the blocked tool call now with exactly the same arguments."
	case StatusConsumed:
		return "This approval was already used by a retry. Retrying the blocked call starts a new request."
	case StatusDenied:
		return "An approver denied this request. Do not retry; tell the user." + noteSuffix(row)
	case StatusExpired:
		return "This request expired before it was used. Tell the user; retrying the blocked call starts a new request."
	case StatusBlocked, StatusPending:
	}

	remaining := defaultRequestTTL
	expires := "when its window ends"
	if row.ExpiresAt.Valid {
		remaining = max(row.ExpiresAt.Time.Sub(r.service.now()), 0)
		expires = "at " + row.ExpiresAt.Time.UTC().Format("2006-01-02 15:04 UTC")
	}
	return fmt.Sprintf(`Approval requested (status: %s). An approver has been asked. The request expires %s.
Do not retry the blocked call yet. Wait for the decision with this command. It checks every %d seconds and prints one line when the request is decided:

%s

In Claude Code, run it with the Monitor tool (description: Waiting for approval, timeout_ms: %d), then end your turn. Its one line of output wakes you. If the Monitor times out first, start it again with the same command.
In Codex, OpenClaw and other agents, run it in a background terminal and keep this turn open until it prints. Do not end your turn while it runs, because nothing wakes you when it finishes.
When it prints APPROVED, retry the blocked call with exactly the same arguments. On anything else, do not retry; tell the user.
`, status, expires, watchIntervalSeconds, r.service.waitCommand(row.Token), monitorTimeoutMs(remaining))
}

// monitorTimeoutMs covers the rest of the request's window plus a minute, so
// the wait loop prints EXPIRED before the Monitor times out. It is capped at
// an hour, the longest Monitor timeout Claude Code allows. A Monitor that
// still times out first is started again, as the instructions say.
func monitorTimeoutMs(remaining time.Duration) int {
	return int(min(remaining+time.Minute, time.Hour) / time.Millisecond)
}

// Handler serves the Approvals MCP server at MCPPath, where it authenticates
// each caller itself.
func (r *Runtime) Handler() http.Handler {
	return r.serve(r.authenticate)
}

// GatewayHandler serves the Approvals MCP server at each project's approvals
// server endpoint. The gateway authenticates callers before it hands requests
// over.
func (r *Runtime) GatewayHandler() http.Handler {
	return r.serve(func(_ http.ResponseWriter, req *http.Request) (context.Context, error) {
		return req.Context(), nil
	})
}

func (r *Runtime) serve(authenticate Authenticator) http.Handler {
	handler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return r.server
	}, &mcp.StreamableHTTPOptions{
		Stateless:                    true,
		JSONResponse:                 true,
		Logger:                       nil,
		EventStore:                   nil,
		SessionTimeout:               0,
		DisableLocalhostProtection:   false,
		CrossOriginProtection:        nil,
		MaxRequestBodyBytes:          mcpMaxBodyBytes,
		PropagateRequestCancellation: false,
	})
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if req.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ctx, err := authenticate(w, req)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, req.WithContext(ctx))
	})
}

// watchResponse is the watch endpoint's JSON body.
type watchResponse struct {
	// Status is the request's status as a watcher sees it.
	Status string `json:"status"`
}

// watchLines is the plain-text watch response for each status. The wait loop
// from waitCommand stops on any line but PENDING.
var watchLines = map[Status]string{
	StatusBlocked:  "BLOCKED: approval was not requested yet; request it with the curl command from the block message",
	StatusPending:  "PENDING",
	StatusApproved: "APPROVED: retry the blocked tool call now with exactly the same arguments",
	StatusConsumed: "APPROVED: retry the blocked tool call now with exactly the same arguments",
	StatusDenied:   "DENIED: an approver denied this request; do not retry; tell the user",
	StatusExpired:  "EXPIRED: nobody decided in time; do not retry; tell the user",
}

// bearerToken reads a request token from the Authorization header. Tokens are
// read only from the header so they never appear in a URL.
func bearerToken(req *http.Request) (string, bool) {
	kind, token, ok := strings.Cut(req.Header.Get("Authorization"), " ")
	token = strings.TrimSpace(token)
	if !ok || !strings.EqualFold(kind, "Bearer") || !IsToken(token) {
		return "", false
	}
	return token, true
}

// allow applies a per-token rate limit. A nil limiter allows everything.
func (r *Runtime) allow(ctx context.Context, w http.ResponseWriter, limiter *ratelimit.Limiter, token string) (bool, error) {
	if limiter == nil {
		return true, nil
	}
	res, err := limiter.Allow(ctx, tokenKey(token))
	if err != nil {
		return false, fmt.Errorf("check rate limit: %w", err)
	}
	if !res.Allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(res.RetryAfter.Seconds())+1))
	}
	return res.Allowed, nil
}

// HandleWatch reports one request's status to its watcher. The bearer token is
// the request token. With ?format=text it answers with one plain-text line,
// even for errors, which the wait loop from waitCommand reads; otherwise with
// JSON. An unknown token gets 401.
func (r *Runtime) HandleWatch(w http.ResponseWriter, req *http.Request) error {
	ctx := req.Context()
	w.Header().Set("Cache-Control", "no-store")
	if req.URL.Query().Get("format") == "text" {
		return r.watchText(ctx, w, req)
	}
	token, ok := bearerToken(req)
	if !ok {
		return oops.E(oops.CodeUnauthorized, nil, "missing request token: send the apr_ token from the block message in the header \"Authorization: Bearer apr_...\", not in the URL")
	}
	allowed, err := r.allow(ctx, w, r.watchLimiter, token)
	if err != nil {
		return oops.E(oops.CodeUnavailable, err, "approval status temporarily unavailable").LogError(ctx, r.service.logger)
	}
	if !allowed {
		return oops.E(oops.CodeRateLimitExceeded, nil, "too many approval status checks")
	}
	status, err := r.service.WatchStatus(ctx, token)
	if errors.Is(err, ErrNotFound) {
		return oops.E(oops.CodeUnauthorized, nil, "unknown request token: send the exact apr_ token from the block message as \"Authorization: Bearer apr_...\"")
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "read approval status").LogError(ctx, r.service.logger)
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(watchResponse{Status: string(status)}); err != nil {
		return fmt.Errorf("write approval status: %w", err)
	}
	return nil
}

// watchText answers a plain-text watch. Lines the wait loop must not stop on,
// such as a rate limit or an outage, start with WAIT.
func (r *Runtime) watchText(ctx context.Context, w http.ResponseWriter, req *http.Request) error {
	token, ok := bearerToken(req)
	if !ok {
		return writeLine(w, http.StatusUnauthorized, `INVALID: missing request token; send the apr_ token from the block message as the header "Authorization: Bearer apr_..."; tell the user if this keeps happening`)
	}
	allowed, err := r.allow(ctx, w, r.watchLimiter, token)
	if err != nil {
		r.service.logger.ErrorContext(ctx, "check approval watch rate limit", attr.SlogError(err))
		return writeLine(w, http.StatusServiceUnavailable, "WAIT: approval status temporarily unavailable")
	}
	if !allowed {
		return writeLine(w, http.StatusTooManyRequests, "WAIT: too many approval status checks")
	}
	status, err := r.service.WatchStatus(ctx, token)
	if errors.Is(err, ErrNotFound) {
		return writeLine(w, http.StatusUnauthorized, "INVALID: unknown request token; tell the user")
	}
	if err != nil {
		r.service.logger.ErrorContext(ctx, "read approval status", attr.SlogError(err))
		return writeLine(w, http.StatusInternalServerError, "WAIT: approval status temporarily unavailable")
	}
	line, ok := watchLines[status]
	if !ok {
		return writeLine(w, http.StatusInternalServerError, "WAIT: approval status temporarily unavailable")
	}
	return writeLine(w, http.StatusOK, line)
}

// requestBody is the JSON an agent posts to the request endpoint.
type requestBody struct {
	// Reason is shown to approvers.
	Reason string `json:"reason"`

	// ExpiresIn is how long to wait for a decision, in seconds. Zero means
	// the default of 30 minutes.
	ExpiresIn int `json:"expires_in"`
}

// HandleRequest files the approval request for a held call. The request token
// from the block message is the only credential: it names one held call, and
// only that caller's retry of the same call can use the approval. The answer
// is plain text telling the agent how to wait for the decision.
func (r *Runtime) HandleRequest(w http.ResponseWriter, req *http.Request) error {
	ctx := req.Context()
	w.Header().Set("Cache-Control", "no-store")
	token, ok := bearerToken(req)
	if !ok {
		return oops.E(oops.CodeUnauthorized, nil, "missing request token: send the apr_ token from the block message in the header \"Authorization: Bearer apr_...\"")
	}
	allowed, err := r.allow(ctx, w, r.requestLimiter, token)
	if err != nil {
		return oops.E(oops.CodeUnavailable, err, "approval requests temporarily unavailable").LogError(ctx, r.service.logger)
	}
	if !allowed {
		return oops.E(oops.CodeRateLimitExceeded, nil, "too many approval requests for this token")
	}
	var body requestBody
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, requestMaxBodyBytes)).Decode(&body); err != nil {
		return oops.E(oops.CodeBadRequest, err, `send a JSON body such as {"reason":"Why this call is needed."}`)
	}
	// Check the bounds in seconds first, so a huge value cannot wrap around
	// when it becomes a Duration.
	if body.ExpiresIn != 0 && (body.ExpiresIn < int(minRequestTTL/time.Second) || body.ExpiresIn > int(maxRequestTTL/time.Second)) {
		return oops.E(oops.CodeBadRequest, nil, "expires_in must be between %d and %d seconds", int(minRequestTTL/time.Second), int(maxRequestTTL/time.Second))
	}
	row, status, err := r.service.Request(ctx, RequestInput{
		Token:          token,
		RequesterURN:   "",
		OrganizationID: "",
		Reason:         body.Reason,
		Scope:          "",
		ExpiresIn:      time.Duration(body.ExpiresIn) * time.Second,
	})
	switch {
	case errors.Is(err, ErrNotFound):
		return oops.E(oops.CodeUnauthorized, nil, "unknown request token: use the exact apr_ token from the block message")
	case errors.Is(err, ErrInvalidRequest):
		return oops.E(oops.CodeBadRequest, nil, "%s", err.Error())
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "the approval request could not be recorded; try again").LogError(ctx, r.service.logger)
	}
	return writeLine(w, http.StatusOK, r.requestResultText(row, status))
}

// writeLine writes a plain-text answer.
func writeLine(w http.ResponseWriter, code int, text string) error {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if _, err := io.WriteString(w, text); err != nil {
		return fmt.Errorf("write approval answer: %w", err)
	}
	return nil
}

// tokenKey names a token in rate-limit keys without storing the token itself.
func tokenKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:16])
}

func noteSuffix(row repo.RiskPolicyApproval) string {
	if !row.DecisionNote.Valid || row.DecisionNote.String == "" {
		return ""
	}
	// The note is a person's free text. Quote it as data so it cannot read as
	// an instruction that overrides the denial.
	return fmt.Sprintf(" The approver left this note, quoted as written: %q", row.DecisionNote.String)
}
