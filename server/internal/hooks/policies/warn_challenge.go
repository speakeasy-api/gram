package policies

import (
	"context"
	"time"

	"github.com/speakeasy-api/gram/server/internal/risk"
)

// WarnAckFunc reports whether the user has a live acknowledgement for a warn
// (challenge) match, so the challenged call should be let through. Only
// meaningful when scan.Action == "warn". The hooks Enforcer's
// WarnAcknowledged has this shape.
type WarnAckFunc func(ctx context.Context, req *Request, actor Actor, scan *risk.ScanResult, toolName string, at time.Time) bool

// WarnDenyFunc records a warn challenge and returns the two framings of the
// deny: the model-facing agentReason (no ack link) and the human-facing
// userReason carrying the out-of-band acknowledgement link. ok=false means
// an ack link could not be produced (missing site URL / cache / user id) —
// the gates MUST fall back to a plain block (fail-safe): a warn must never
// silently allow. The hooks Enforcer's WarnDenyReason has this shape.
type WarnDenyFunc func(ctx context.Context, req *Request, actor Actor, scan *risk.ScanResult, toolName string, at time.Time) (agentReason, userReason string, ok bool)
