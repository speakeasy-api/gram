package assistants

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// bootstrapRateBurst caps how many bootstrap calls one assistant can fire
// in quick succession. Steady state is once per thread per VM lifetime,
// so anything sustained above this signals either a bug (runner thrash)
// or token abuse.
const (
	bootstrapRateBurst    = 60
	bootstrapRatePerMin   = 60
	bootstrapMaxBodyBytes = 4 * 1024
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

	// A Store outage is not a throttle — fail open rather than wedge bootstrap.
	switch res, err := s.bootstrapLimiter.Allow(ctx, principal.AssistantID.String()); {
	case err != nil:
		s.logger.WarnContext(ctx, "bootstrap rate limiter unavailable, allowing",
			attr.SlogError(err),
			attr.SlogAssistantID(principal.AssistantID.String()),
		)
	case !res.Allowed:
		return oops.E(oops.CodeRateLimitExceeded, nil, "thread bootstrap rate limit exceeded")
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
