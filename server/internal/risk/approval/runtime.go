package approval

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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

	// WatchPath is the endpoint the watcher polls with a request token.
	WatchPath = "/approvals/watch"

	// MCPServerName is the server name agents see.
	MCPServerName = "speakeasy-approvals"

	// watchIntervalSeconds is how often the watcher polls. Three seconds
	// feels immediate to a person who just clicked Approve and stays well
	// under the per-token rate limit.
	watchIntervalSeconds = 3

	// watchMarginChecks lets the watcher outlast the request window by a
	// minute, so it reports EXPIRED instead of timing out.
	watchMarginChecks = 20

	// mcpMaxBodyBytes bounds one MCP request. request_approval carries a token,
	// a sentence and two small options; 64 KiB leaves room for protocol framing.
	mcpMaxBodyBytes = 64 << 10 // 64 KiB
)

// watchScript is the watcher shipped verbatim in every request_approval
// result.
//
//go:embed approval-watch.sh
var watchScript string

// Authenticator resolves the caller of an Approvals MCP request into an
// authenticated context, using the same credentials as gated MCP servers.
type Authenticator func(w http.ResponseWriter, r *http.Request) (context.Context, error)

// Runtime serves the Approvals MCP server and the watch endpoint.
type Runtime struct {
	service      *Service
	authenticate Authenticator
	watchLimiter *ratelimit.Limiter
	server       *mcp.Server
}

// watchRate bounds polling per token. The watcher polls every 3 seconds (20 a
// minute); 60 a minute leaves room for restarts and a second watcher.
var watchRate = ratelimit.PerMinute(60)

// NewRuntime builds the Approvals MCP server. A nil store disables watch rate
// limiting.
func NewRuntime(service *Service, authenticate Authenticator, store ratelimit.Store) *Runtime {
	var limiter *ratelimit.Limiter
	if store != nil {
		limiter = ratelimit.New(store, "approval-watch", watchRate)
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
	r := &Runtime{service: service, authenticate: authenticate, watchLimiter: limiter, server: server}
	mcp.AddTool(server, &mcp.Tool{
		Name:         "request_approval",
		Title:        "Request approval for a blocked tool call",
		Description:  "Ask a person to approve a tool call that a Speakeasy approval policy blocked. Pass the request token from the block message and one sentence explaining why the call is needed. Returns the steps and the watcher script to wait for the decision; follow them exactly.",
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: new(false), IdempotentHint: true, OpenWorldHint: new(false), Title: ""},
		Meta:         nil,
		InputSchema:  nil,
		OutputSchema: nil,
		Icons:        nil,
	}, r.requestApproval)
	return r
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

	id := shortID(row.Token)
	tokenFile := "$HOME/.speakeasy-approval-" + id + ".token"
	checks := r.watchChecks(row)
	expires := "when its window ends"
	if row.ExpiresAt.Valid {
		expires = "at " + row.ExpiresAt.Time.UTC().Format("2006-01-02 15:04 UTC")
	}
	command := fmt.Sprintf(`bash approval-watch.sh %s "%s" %d %d`, r.service.WatchURL(), tokenFile, watchIntervalSeconds, checks)
	return fmt.Sprintf(`Approval requested (status: %s). An approver has been asked. The request expires %s.
Do not retry the blocked call yet. Start the watcher below and retry with exactly the same arguments only when it prints APPROVED.

1. Save the request token, and nothing else, to %s with your file-writing tool. If you have none, run:
   umask 077 && printf '%%s' '%s' > "%s"
2. Save the script at the end of this message as approval-watch.sh in your working directory.
3. Start the watcher. Do not write your own poller: the watch URL only accepts the token as an "Authorization: Bearer apr_..." header, and the script sends it.
   In Claude Code, call the Monitor tool with:
     command: %s
     description: Waiting for approval %s
     timeout_ms: %d
   Then you can end your turn. The watcher prints a line only when the status changes, so its last line wakes you. If the Monitor times out first, start it again with the same command.
   In Codex, OpenClaw and other agents, run the same command in a background terminal and keep this turn open: wait on its output until it exits. Do not end your turn while it runs, because nothing wakes you when it finishes.
4. When it prints APPROVED, retry the blocked call with exactly the same arguments. On DENIED, EXPIRED, WATCH FAILED or WATCH TIMEOUT, do not retry; tell the user.
5. Once the watcher has exited, delete the token file and the script: rm -f "%s" approval-watch.sh

approval-watch.sh:
%s`, status, expires, tokenFile, row.Token, tokenFile, command, id, monitorTimeoutMs(checks), tokenFile, watchScript)
}

// monitorTimeoutMs covers the watcher's run plus a minute, capped at an hour,
// the longest Monitor timeout Claude Code allows.
func monitorTimeoutMs(checks int) int {
	return min((checks*watchIntervalSeconds+60)*1000, 3_600_000)
}

// watchChecks is how many polls cover the rest of the request's window, plus
// a margin so the watcher sees the final status rather than timing out first.
func (r *Runtime) watchChecks(row repo.RiskPolicyApproval) int {
	remaining := defaultRequestTTL
	if row.ExpiresAt.Valid {
		remaining = max(row.ExpiresAt.Time.Sub(r.service.now()), 0)
	}
	return int(remaining/(watchIntervalSeconds*time.Second)) + watchMarginChecks
}

// Handler serves the Approvals MCP server.
func (r *Runtime) Handler() http.Handler {
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
		ctx, err := r.authenticate(w, req)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, req.WithContext(ctx))
	})
}

// watchResponse is the watch endpoint's body.
type watchResponse struct {
	// Status is the request's status as a watcher sees it.
	Status string `json:"status"`
}

// HandleWatch reports one request's status to its watcher. The bearer token is
// the request token; it is read only from the Authorization header so it never
// appears in a URL. An unknown token gets 401.
func (r *Runtime) HandleWatch(w http.ResponseWriter, req *http.Request) error {
	ctx := req.Context()
	w.Header().Set("Cache-Control", "no-store")
	kind, token, ok := strings.Cut(req.Header.Get("Authorization"), " ")
	token = strings.TrimSpace(token)
	if !ok || !strings.EqualFold(kind, "Bearer") || !IsToken(token) {
		return oops.E(oops.CodeUnauthorized, nil, "missing request token: send the apr_ token from the block message in the header \"Authorization: Bearer apr_...\", not in the URL. The approval-watch.sh script from request_approval does this")
	}
	if r.watchLimiter != nil {
		res, err := r.watchLimiter.Allow(ctx, tokenKey(token))
		if err != nil {
			return oops.E(oops.CodeUnavailable, err, "approval status temporarily unavailable").LogError(ctx, r.service.logger)
		}
		if !res.Allowed {
			w.Header().Set("Retry-After", strconv.Itoa(int(res.RetryAfter.Seconds())+1))
			return oops.E(oops.CodeRateLimitExceeded, nil, "too many approval status checks")
		}
	}
	status, err := r.service.WatchStatus(ctx, token)
	if errors.Is(err, ErrNotFound) {
		return oops.E(oops.CodeUnauthorized, nil, "unknown request token: send the exact apr_ token from the block message as \"Authorization: Bearer apr_...\". The approval-watch.sh script from request_approval does this")
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

// tokenKey names a token in rate-limit keys without storing the token itself.
func tokenKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:16])
}

// shortID is a short, non-secret label for a token, used in file names.
func shortID(token string) string {
	return tokenKey(token)[:8]
}

func noteSuffix(row repo.RiskPolicyApproval) string {
	if !row.DecisionNote.Valid || row.DecisionNote.String == "" {
		return ""
	}
	// The note is a person's free text. Quote it as data so it cannot read as
	// an instruction that overrides the denial.
	return fmt.Sprintf(" The approver left this note, quoted as written: %q", row.DecisionNote.String)
}
