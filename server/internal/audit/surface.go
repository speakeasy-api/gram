package audit

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// Surface names how a change was made.
//
// The actor fields answer who made a change; a surface answers through what.
// Without one, the same user adding an MCP server from the dashboard and from
// an agent over Platform MCP writes identical rows, and an administrator
// reviewing a runaway agent's burst of activity cannot separate it from their
// own work.
//
// The set is deliberately small and closed. Values reach the database only
// through this type, so the column stays low-cardinality no matter what a
// caller supplies.
type Surface string

const (
	// SurfaceUnknown is recorded when a request reached us and nothing in it
	// identified a surface. It is an admission rather than a fallback: rows
	// written before this field existed, and requests whose identity we failed
	// to classify, are honestly unattributed instead of being guessed at.
	//
	// It does not mean "no request". Background work marks itself
	// SurfaceSystem, so an unknown surface is a gap worth chasing rather than
	// the expected shape of a scheduled job.
	SurfaceUnknown Surface = "unknown"
	// SurfaceSystem is Gram acting on its own, with no request behind it: a
	// Temporal activity, a scheduled job, a reconciler. These writes have no
	// session, API key or human actor to attribute, and saying so is more
	// useful than leaving them indistinguishable from a classification miss.
	SurfaceSystem Surface = "system"
	// SurfaceDashboard is an authenticated dashboard session.
	SurfaceDashboard Surface = "dashboard"
	// SurfaceAPIKey is a Gram API key, used by the CLI and by automation.
	SurfaceAPIKey Surface = "api_key"
	// SurfacePlatformMCP is the OAuth-authenticated Platform MCP endpoint,
	// where a third-party agent acts on a user's behalf.
	SurfacePlatformMCP Surface = "platform_mcp"
	// SurfaceProjectAssistant is a project's managed assistant acting during a
	// user's turn.
	SurfaceProjectAssistant Surface = "project_assistant"
	// SurfaceAdmin is the isolated Google-authenticated admin app.
	SurfaceAdmin Surface = "admin"
	// SurfacePlatformBreakGlass is the main-server platform-administrator
	// killswitch recovery path. It remains distinct from customer management.
	SurfacePlatformBreakGlass Surface = "platform_break_glass"
)

// knownSurfaces is the allowlist an explicitly marked surface is checked
// against. A package marking a surface passes a plain string so it need not
// depend on this one; anything not named here records an unknown surface
// rather than widening what the column can hold.
var knownSurfaces = map[Surface]struct{}{
	SurfaceUnknown:            {},
	SurfaceSystem:             {},
	SurfaceDashboard:          {},
	SurfaceAPIKey:             {},
	SurfacePlatformMCP:        {},
	SurfaceProjectAssistant:   {},
	SurfaceAdmin:              {},
	SurfacePlatformBreakGlass: {},
}

// actingIdentity is how a change was made: the surface, and the registered
// OAuth client it came through when there was one.
type actingIdentity struct {
	Surface  Surface
	ClientID string
}

// actingIdentityFromContext derives how the current request is acting.
//
// Derivation happens here, at the moment of the write, rather than being
// stamped when the request authenticates. By this point the context carries
// every signal the request accumulated, so an assistant acting during a user's
// turn is not mistaken for the dashboard session its token arrived on.
//
// Order matters. Each check is more specific than the one below it:
//
//   - an explicit mark wins, because only a surface that authenticates its own
//     way needs to set one, and it knows better than any inference here;
//   - an assistant principal means the assistant runtime is acting, whatever
//     credential carried the request in;
//   - an admin session means the isolated admin app is acting;
//   - an API key is a distinct surface from the dashboard, and calling it
//     "dashboard" would misreport CLI and automation writes;
//   - a session is the dashboard.
//
// Nothing matching leaves the surface unknown.
func actingIdentityFromContext(ctx context.Context) actingIdentity {
	clientID, _ := contextvalues.GetOAuthClientID(ctx)

	if marked, ok := contextvalues.GetActingSurface(ctx); ok {
		surface := Surface(marked)
		if _, known := knownSurfaces[surface]; known {
			return actingIdentity{Surface: surface, ClientID: clientID}
		}
		return actingIdentity{Surface: SurfaceUnknown, ClientID: clientID}
	}

	if _, ok := contextvalues.GetAssistantPrincipal(ctx); ok {
		return actingIdentity{Surface: SurfaceProjectAssistant, ClientID: clientID}
	}

	if adminAuthCtx, ok := contextvalues.GetAdminAuthContext(ctx); ok && adminAuthCtx.SessionID != "" {
		return actingIdentity{Surface: SurfaceAdmin, ClientID: clientID}
	}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return actingIdentity{Surface: SurfaceUnknown, ClientID: clientID}
	}
	switch {
	case authCtx.APIKeyID != "":
		return actingIdentity{Surface: SurfaceAPIKey, ClientID: clientID}
	case authCtx.SessionID != nil && *authCtx.SessionID != "":
		return actingIdentity{Surface: SurfaceDashboard, ClientID: clientID}
	default:
		return actingIdentity{Surface: SurfaceUnknown, ClientID: clientID}
	}
}

// SurfaceFromContext is how a request is acting, for callers that must carry
// the surface across a boundary a context cannot cross.
//
// Audit normally derives this at the moment of the write, which is where the
// signals are richest. Work handed to Temporal breaks that: the activity runs
// in a worker with none of the request's context, so a caller that schedules
// work on a user's behalf has to capture the surface at schedule time and pass
// it through the workflow payload the way it already passes the actor.
//
// Prefer letting audit derive the surface wherever the write happens in the
// request's own goroutine. Reach for this only at a handoff.
func SurfaceFromContext(ctx context.Context) Surface {
	return actingIdentityFromContext(ctx).Surface
}
