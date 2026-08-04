package policies

import (
	"context"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// Actor is the human the event is attributed to. Distinct from the
// AuthContext identity: an org-wide plugin key authenticates many machines,
// so its owner (the publishing admin) must not absorb every developer's
// telemetry.
type Actor struct {
	UserID string
	Email  string
}

// Request carries the per-request ingest state the policy stages need beyond
// the typed event: the validated canonical payload (the typed events project
// only the decision-relevant fields), the authenticated context, and the
// actor Ingest resolved for attribution. The tool projections (name, input,
// MCP predicate) ride on the typed event itself — see the ToolCall the
// tool-ish events carry and the stamped extension read by mcpToolRequest.
// Request rides on ctx so stages keep the plain agenthooks handler
// signatures.
type Request struct {
	// Payload is the validated hook.ingest.v1 payload the event was decoded
	// from.
	Payload *gen.IngestPayload
	// AuthCtx is the authenticated context of the ingest request.
	AuthCtx *contextvalues.AuthContext
	// Actor is the identity Ingest resolved for attribution. Stages read it
	// through ActorFromContext (installed by the ActorResolution middleware),
	// not from here, so they depend on "the actor is in ctx" rather than on
	// how it got there.
	Actor Actor
	// AllowWarnAck reports whether this ingest surface supports the
	// out-of-band warn-acknowledgement flow for prompt challenges
	// (AuthenticatedIngestOptions.AllowWarnAcknowledgement, projected by
	// Ingest). When false a prompt warn falls straight through to a plain
	// block; the tool-flavored gates consult the acknowledgement flow
	// unconditionally, exactly as the inline evaluation did.
	AllowWarnAck bool
}

type requestKey struct{}

// WithRequest stashes the ingest request state for the policy stages.
func WithRequest(ctx context.Context, req *Request) context.Context {
	return context.WithValue(ctx, requestKey{}, req)
}

// RequestFromContext returns the ingest request state installed with
// WithRequest, or nil when the event was not dispatched through the ingest
// path. Stages treat a nil request as "stay neutral".
func RequestFromContext(ctx context.Context) *Request {
	req, _ := ctx.Value(requestKey{}).(*Request)
	return req
}
