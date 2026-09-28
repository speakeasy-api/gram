// The consent-page probe negotiates with the official MCP SDK through the member's proxy.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// probeReserve is the most a probe keeps back from ValidationTimeout, and a
// quarter of the budget the least: half a floor for the session close after
// the deadline, half for the verdict write after that.
const probeReserve = 2 * time.Second

// probeUpstream negotiates with the member and lists its tools through the
// same SDK client and proxy used by dispatch, inside ValidationTimeout less
// the close floor and the verdict's write window. A 401 or 403 after the SDK's optional
// discovery leg is a rejection, a tool list is valid, and anything else is
// unknown. Closing the session is the SDK's DELETE.
func (s *Service) probeUpstream(ctx context.Context, logger *slog.Logger, build memberProxyBuilder, name string) (remotesessions.ValidationOutcome, string) {
	timeout := s.metaRuntime.ValidationTimeout
	if parentDeadline, ok := ctx.Deadline(); ok {
		timeout = min(timeout, time.Until(parentDeadline))
	}
	reserve := min(probeReserve, timeout/4)
	deadline := time.Now().Add(max(timeout-reserve, time.Millisecond))
	probeCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	session, rt, err := s.connectMetaMember(probeCtx, logger, build, reserve/2)
	if err != nil {
		return classifyProbe(probeCtx, rt, err, name)
	}
	_, err = session.ListTools(probeCtx, nil)
	if cerr := session.Close(); cerr != nil {
		logger.DebugContext(ctx, "close probe session", attr.SlogError(cerr))
	}
	if err != nil {
		return classifyProbe(probeCtx, rt, err, name)
	}
	return remotesessions.ValidationOutcomeValid, ""
}

// classifyProbe maps a failed leg to a verdict and a fixed-phrase reason; nothing upstream sent is quoted.
func classifyProbe(ctx context.Context, rt *memberRoundTripper, err error, name string) (remotesessions.ValidationOutcome, string) {
	if _, rejected := rt.rejection(); rejected {
		return remotesessions.ValidationOutcomeRejectedByMember, "Rejected by " + name
	}
	switch status := rt.status(); {
	case errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded):
		return remotesessions.ValidationOutcomeUnknown, name + " did not answer in time"
	case errors.Is(err, errMemberResponseTooLarge):
		return remotesessions.ValidationOutcomeUnknown, "Unexpected answer from " + name
	case status == 0:
		return remotesessions.ValidationOutcomeUnknown, "Could not reach " + name
	case !upstreamStatusOK(status):
		return remotesessions.ValidationOutcomeUnknown, fmt.Sprintf("%s answered with status %d", name, status)
	default:
		return remotesessions.ValidationOutcomeUnknown, "Unexpected answer from " + name
	}
}
