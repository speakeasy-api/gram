package mcpriskscan

import (
	"context"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
)

// MaxPayloadBytes is the synchronous inspection budget, not a request limit.
// Larger requests still execute, but their payload is unavailable to risk
// evaluation at this seam.
const MaxPayloadBytes = 50 * 1024

const (
	SurfaceHostedMCP   = "hosted_mcp"
	SurfacePlatformMCP = "platform_mcp"
	SurfaceInstances   = "instances"
	SurfaceRemoteMCP   = "remote_mcp"

	MethodToolsCall     = "tools/call"
	MethodResourcesRead = "resources/read"
	MethodPromptsGet    = "prompts/get"

	PhaseRequest  = "request"
	PhaseResponse = "response"
)

// PayloadAvailability explains why inspection bytes are absent.
type PayloadAvailability string

const (
	// PayloadUnavailable means the seam has no meaningful materialized payload.
	PayloadUnavailable PayloadAvailability = "unavailable"
	// PayloadAvailable means the complete payload fits within the inspection budget.
	PayloadAvailable PayloadAvailability = "available"
	// PayloadOversized means the complete payload exceeds the inspection budget.
	PayloadOversized PayloadAvailability = "oversized"
)

// Payload holds complete inspection bytes only. Its zero value is unavailable.
// Bytes are borrowed from the caller and valid only during Evaluator.Scan; an
// observer must not mutate or retain them, including from a telemetry defer or
// goroutine. A stream or reader is deliberately not representable.
type Payload struct {
	data         []byte
	availability PayloadAvailability
}

// Bytes returns the borrowed complete payload, or nil when unavailable.
func (p Payload) Bytes() []byte { return p.data }

// Availability distinguishes absent content from content too large to inspect.
func (p Payload) Availability() PayloadAvailability {
	if p.availability == "" {
		return PayloadUnavailable
	}
	return p.availability
}

// BorrowPayload never copies or truncates bytes, and never changes the request.
func BorrowPayload(data []byte) Payload {
	if data == nil {
		return Payload{data: nil, availability: PayloadUnavailable}
	}
	if len(data) > MaxPayloadBytes {
		return Payload{data: nil, availability: PayloadOversized}
	}
	return Payload{data: data[:len(data):len(data)], availability: PayloadAvailable}
}

// Event is identifiers-only metadata that may be retained independently of the
// synchronous payload. MetaServerID is route attribution; it never replaces the
// concrete ServerID or changes Surface.
type Event struct {
	Surface         string
	Method          string
	OrganizationID  string
	ProjectID       string
	ServerID        string
	MetaServerID    string
	ToolsetID       string
	ToolName        string
	ResourceURI     string
	PromptName      string
	ChatID          string
	phase           string
	executionID     string
	principal       mcpidentity.Identity
	identityStamped bool
}

// Phase returns the inspection phase assigned by the subject constructor.
func (e Event) Phase() string { return e.phase }

// ExecutionID correlates request and response subjects for one execution.
func (e Event) ExecutionID() string { return e.executionID }

// Principal returns provenance captured exclusively from mcpidentity.
func (e Event) Principal() mcpidentity.Identity { return e.principal }

// IdentityStamped reports whether mcpidentity supplied validated provenance.
func (e Event) IdentityStamped() bool { return e.identityStamped }

// Subject is the one value a mediation seam hands to risk evaluation. It keeps
// retainable identifiers separate from bounded, synchronous-only payload bytes.
// The private ownership and claim state make one concrete seam authoritative and
// make repeated scans of the same phase a no-op.
type Subject struct {
	Event   Event
	Payload Payload `json:"-"`

	evaluationOwner bool
	claimed         *atomic.Bool
	responseClaim   *atomic.Bool
}

// NewRequest captures trusted principal provenance and creates the request-phase
// subject owned by a supported concrete mediation seam. Unknown surface/method
// pairs are observation-only and Evaluator.Scan ignores them; there is no meta
// surface owner because a meta route delegates to its concrete hosted or remote
// member seam.
func NewRequest(ctx context.Context, event Event, payload Payload) Subject {
	event.phase = PhaseRequest
	event.executionID = uuid.NewString()
	event.principal, event.identityStamped = mcpidentity.FromContext(ctx)
	return Subject{
		Event:           event,
		Payload:         payload,
		evaluationOwner: ownsRequestEvaluation(event.Surface, event.Method),
		claimed:         new(atomic.Bool),
		responseClaim:   new(atomic.Bool),
	}
}

// NewResponse derives a response-phase subject from its request without
// accepting a response stream. Repeated derivations share one response claim,
// so duplicate same-id terminal events cannot evaluate twice. A future response
// adapter must pass one bounded, materialized terminal message. For SSE,
// progress, EOF, cancellation, malformed events, and missing terminal messages
// are not whole-stream enforcement points.
func NewResponse(request Subject, payload Payload) Subject {
	event := request.Event
	event.phase = PhaseResponse
	return Subject{
		Event:           event,
		Payload:         payload,
		evaluationOwner: request.evaluationOwner,
		claimed:         request.responseClaim,
		responseClaim:   request.responseClaim,
	}
}

// EvaluationOwner reports whether this is the authoritative concrete seam.
func (s Subject) EvaluationOwner() bool { return s.evaluationOwner }

func (s Subject) claimEvaluation() bool {
	return s.evaluationOwner && s.claimed != nil && s.claimed.CompareAndSwap(false, true)
}

func ownsRequestEvaluation(surface, method string) bool {
	switch surface {
	case SurfaceHostedMCP:
		return method == MethodToolsCall || method == MethodResourcesRead || method == MethodPromptsGet
	case SurfacePlatformMCP, SurfaceInstances, SurfaceRemoteMCP:
		return method == MethodToolsCall
	default:
		return false
	}
}
