package assistants

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
)

// Bootstrap authenticates each invocation, including retries. Bound per-thread
// credentials get independent buckets so busy siblings cannot starve each other.
// Legacy assistant-wide credentials retain the assistant-wide abuse limit.
const (
	bootstrapRateBurst    = 60
	bootstrapRatePerMin   = 60
	bootstrapMaxBodyBytes = 4 * 1024
	// Schema caps max_concurrency at 100. Each turn attempt makes one
	// bootstrap call. Allow all 100 slots at the per-thread sustained rate,
	// but only one simultaneous wave, not 60 bursts for every thread ID.
	bootstrapAggregateBurst      = 100
	bootstrapAggregateRatePerMin = 100 * bootstrapRatePerMin
)

type bootstrapRequest struct {
	ThreadID string `json:"thread_id"`
}

func (s *Service) handleGetThreadBootstrap(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	token := r.Header.Get("Authorization")
	if token == "" {
		return oops.C(oops.CodeUnauthorized)
	}

	authedCtx, claims, err := s.core.assistantTokens.Authorize(ctx, token)
	if err != nil {
		return fmt.Errorf("authorize assistant runtime token: %w", err)
	}
	ctx = authedCtx

	principal, ok := contextvalues.GetAssistantPrincipal(ctx)
	if !ok {
		return oops.C(oops.CodeUnauthorized)
	}

	projectID, err := uuid.Parse(claims.ProjectID)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "invalid token project")
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, bootstrapMaxBodyBytes))
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "read bootstrap request")
	}
	var req bootstrapRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return oops.E(oops.CodeBadRequest, err, "decode bootstrap request")
	}
	threadID, err := uuid.Parse(req.ThreadID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid thread_id")
	}

	// Per-thread token (ThreadID claim populated) may only bootstrap its
	// own thread; rejects replay/misuse against a sibling under the same
	// assistant. Assistant-only tokens (ThreadID zero) still flow through.
	if principal.ThreadID != uuid.Nil && principal.ThreadID != threadID {
		return oops.E(oops.CodeForbidden, nil, "token thread does not match requested thread")
	}

	if err := s.allowBootstrap(ctx, principal.AssistantID, principal.ThreadID); err != nil {
		return err
	}

	result, err := s.core.BuildThreadBootstrap(ctx, projectID, threadID, principal.AssistantID)
	if err != nil {
		return err
	}

	s.logger.InfoContext(ctx, "assistant thread bootstrap served",
		attr.SlogAssistantID(principal.AssistantID.String()),
		attr.SlogAssistantThreadID(threadID.String()),
		attr.SlogProjectID(projectID.String()),
	)

	payload, err := json.Marshal(result)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "encode bootstrap response")
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(payload); err != nil {
		return fmt.Errorf("write bootstrap response: %w", err)
	}
	return nil
}

// Per-thread limits run first so a single hot thread cannot drain the shared
// allowance. Both guards use the existing distributed Store and fail-open
// outage behavior; exceeding either healthy bucket is a real throttle.
func (s *Service) allowBootstrap(ctx context.Context, assistantID, tokenThreadID uuid.UUID) error {
	rateKey := assistantID.String()
	if tokenThreadID != uuid.Nil {
		rateKey += ":" + tokenThreadID.String()
	}
	for _, guard := range []struct {
		limiter *ratelimit.Limiter
		key     string
	}{
		{s.bootstrapLimiter, rateKey},
		{s.bootstrapAggregateLimiter, assistantID.String()},
	} {
		switch res, err := guard.limiter.Allow(ctx, guard.key); {
		case err != nil:
			s.logger.WarnContext(ctx, "bootstrap rate limiter unavailable, allowing", attr.SlogError(err), attr.SlogAssistantID(assistantID.String()))
		case !res.Allowed:
			return oops.E(oops.CodeRateLimitExceeded, nil, "thread bootstrap rate limit exceeded")
		}
	}
	return nil
}
