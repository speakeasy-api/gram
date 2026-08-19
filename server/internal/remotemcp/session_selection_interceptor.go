package remotemcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// SessionSelectionInterceptor enforces a consent-screen session selection
// on both sides of a proxied MCP conversation with one shared predicate: it
// is a [proxy.ToolsListResponseInterceptor] that filters listings down to
// the session's scope, and a [proxy.ToolsCallRequestInterceptor] that
// rejects calls outside it — so a session is never shown a tool it cannot
// invoke, and never invokes a tool it was not shown. Attached only when the
// session carries a restrictive selection, after any RBAC interceptors, so
// the effective catalog is the intersection of RBAC and consent.
//
// A tool is in scope by exact name against the frozen name grant, or — for
// live annotation grants — by the upstream's declared hints. tools/call
// carries no hints, so live grants use listing-witnessed enforcement: the
// list side records the live-matched rows each MCP session was actually
// shown, and the call side authorizes against that witness, falling back to
// a grant-scoped witness when a stateless upstream supplies no MCP session
// id. Concurrent stateless clients on one grant may replace or invalidate
// that shared record; pagination mismatches drop it, so races fail narrow,
// never wide.
type SessionSelectionInterceptor struct {
	selection *toolfilter.SessionSelection
	store     *toolfilter.SessionToolWitnessStore
}

var (
	_ proxy.ToolsListResponseInterceptor = (*SessionSelectionInterceptor)(nil)
	_ proxy.ToolsCallRequestInterceptor  = (*SessionSelectionInterceptor)(nil)
)

// NewSessionSelectionInterceptor builds the selection enforcer. A nil
// selection means all tools and must not be wired through this interceptor
// at all; callers gate on non-nil before attaching, and a nil selection
// reaching here fails closed to zero tools rather than all. store may be
// nil, which disables witnessing — live grants then narrow to the frozen
// name grant on both sides.
func NewSessionSelectionInterceptor(selection *toolfilter.SessionSelection, store *toolfilter.SessionToolWitnessStore) *SessionSelectionInterceptor {
	return &SessionSelectionInterceptor{selection: selection, store: store}
}

// Name implements both interceptor interfaces.
func (i *SessionSelectionInterceptor) Name() string {
	return "session-selection"
}

// sdkAnnotationValues maps go-sdk hint fields to the consent vocabulary,
// keeping only hints that are explicitly true. The SDK models
// readOnly/idempotent as plain bools and destructive/openWorld as *bool
// (their spec defaults are true); a nil pointer is not an explicit true.
func sdkAnnotationValues(annotations *mcp.ToolAnnotations) []string {
	if annotations == nil {
		return nil
	}
	var values []string
	if annotations.ReadOnlyHint {
		values = append(values, toolfilter.AnnotationReadOnly)
	}
	if annotations.DestructiveHint != nil && *annotations.DestructiveHint {
		values = append(values, toolfilter.AnnotationDestructive)
	}
	if annotations.IdempotentHint {
		values = append(values, toolfilter.AnnotationIdempotent)
	}
	if annotations.OpenWorldHint != nil && *annotations.OpenWorldHint {
		values = append(values, toolfilter.AnnotationOpenWorld)
	}
	return values
}

// sdkTypesAnnotations converts go-sdk hints to the gen/types shape
// toolfilter.AnnotationsMatch consumes, preserving explicit-true only.
func sdkTypesAnnotations(annotations *mcp.ToolAnnotations) *types.ToolAnnotations {
	if annotations == nil {
		return nil
	}
	boolPtr := func(v bool) *bool {
		if v {
			return &v
		}
		return nil
	}
	return &types.ToolAnnotations{
		Title:           nil,
		ReadOnlyHint:    boolPtr(annotations.ReadOnlyHint),
		DestructiveHint: annotations.DestructiveHint,
		IdempotentHint:  boolPtr(annotations.IdempotentHint),
		OpenWorldHint:   annotations.OpenWorldHint,
	}
}

// InterceptToolsListResponse implements [proxy.ToolsListResponseInterceptor].
// Upstream JSON-RPC error responses pass through untouched — they carry no
// tool inventory. Successful results are rebuilt in upstream order; an
// empty result is a valid outcome and is committed as an empty array.
// Live-matched survivors are witnessed AFTER the filtered result is
// accepted into the response, so a page that failed filtering never leaves
// call authorization behind. (Delivery to the client can still fail later;
// that does not widen anything — the witness only records tools whose
// upstream-declared hints the live grant already covers.)
func (i *SessionSelectionInterceptor) InterceptToolsListResponse(ctx context.Context, list *proxy.ToolsListResponse) error {
	if list == nil {
		return fmt.Errorf("session tool selection: nil tools/list response")
	}
	if list.Error != nil {
		return nil
	}
	if list.Result == nil {
		return fmt.Errorf("session tool selection: tools/list response carries neither result nor error")
	}

	// The upstream session id scopes stateful conversations. Stateless
	// upstreams leave it empty and deliberately share the witness within this
	// selection's grant; grant id remains part of every store key.
	sessionID := ""
	if list.RemoteMessage != nil && list.RemoteMessage.UserHTTPRequest != nil {
		sessionID = list.RemoteMessage.UserHTTPRequest.Header.Get(proxy.McpSessionIDHeader)
	}
	live := i.selection.LiveAnnotations()
	liveEligible := len(live) > 0 && i.store != nil

	allowed := make([]*mcp.Tool, 0, len(list.Result.Tools))
	witnessed := make([]toolfilter.WitnessedTool, 0, len(list.Result.Tools))
	for _, tool := range list.Result.Tools {
		if tool == nil {
			continue
		}
		if i.selection.AllowsName(tool.Name) {
			allowed = append(allowed, tool)
			continue
		}
		if liveEligible && toolfilter.AnnotationsMatch(sdkTypesAnnotations(tool.Annotations), live) {
			allowed = append(allowed, tool)
			witnessed = append(witnessed, toolfilter.WitnessedTool{
				Name:        tool.Name,
				Annotations: sdkAnnotationValues(tool.Annotations),
			})
		}
	}

	// Commit the filtered result before witnessing: a failed commit must
	// never leave call authorization behind for a page that was not
	// accepted.
	if err := list.SetPrivateTools(allowed); err != nil {
		return fmt.Errorf("commit session-filtered tools/list result: %w", err)
	}

	// Pages with no live-matched rows are still witnessed (empty) so the
	// cursor chain stays intact across them.
	if liveEligible {
		requestCursor := ""
		if list.Request != nil && list.Request.Params != nil {
			requestCursor = list.Request.Params.Cursor
		}
		i.store.WitnessPage(ctx, i.selection.GrantID.String(), sessionID, requestCursor, witnessed, list.Result.NextCursor)
	}
	return nil
}

// InterceptToolsCallRequest implements [proxy.ToolsCallRequestInterceptor].
// A missing call or params is an internal invariant break on a restrictive
// session, not a pass-through: fail closed rather than forward a request
// whose tool name could not be established.
func (i *SessionSelectionInterceptor) InterceptToolsCallRequest(ctx context.Context, call *proxy.ToolsCallRequest) error {
	if call == nil || call.Params == nil {
		return fmt.Errorf("session tool selection: tools/call request carries no params")
	}
	if i.selection.AllowsName(call.Params.Name) {
		return nil
	}
	live := i.selection.LiveAnnotations()
	if i.store != nil && len(live) > 0 {
		sessionID := ""
		if call.UserRequest != nil && call.UserRequest.UserHTTPRequest != nil {
			sessionID = call.UserRequest.UserHTTPRequest.Header.Get(proxy.McpSessionIDHeader)
		}
		if i.store.MatchesWitnessed(ctx, i.selection.GrantID.String(), sessionID, call.Params.Name, live) {
			return nil
		}
	}
	return &proxy.RejectError{
		Code:    proxy.RejectCodeInvalidRequest,
		Message: "tool is not approved for this session",
		Data:    nil,
	}
}
