package policies

import (
	"context"
	"time"

	"github.com/speakeasy-api/gram/server/internal/risk"
)

// WarnChallenger backs the warn (challenge) handling in the risk-scan gates:
// the live-acknowledgement check that lets a challenged call through, and
// the challenge-deny that records the challenge and renders the deny with
// the out-of-band acknowledgement link. ok=false from WarnDenyReason means
// an ack link could not be produced (missing site URL / cache / user id) —
// the gates fall back to a plain block (fail-safe): a warn must never
// silently allow.
type WarnChallenger interface {
	WarnAcknowledged(ctx context.Context, req *Request, actor Actor, scan *risk.ScanResult, toolName string, at time.Time) bool
	WarnDenyReason(ctx context.Context, req *Request, actor Actor, scan *risk.ScanResult, toolName string, at time.Time) (agentReason, userReason string, ok bool)
}
