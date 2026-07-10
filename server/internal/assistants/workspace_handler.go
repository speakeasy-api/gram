package assistants

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	workspaceGrowthRatePerMin = 1
	workspaceGrowthRateBurst  = 1
)

func (s *Service) handleGrowWorkspace(w http.ResponseWriter, r *http.Request) error {
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
	rate, err := s.workspaceGrowthLimiter.Allow(ctx, principal.AssistantID.String())
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "check workspace growth rate limit").LogError(ctx, s.logger,
			attr.SlogAssistantID(principal.AssistantID.String()),
		)
	}
	if !rate.Allowed {
		return oops.E(oops.CodeRateLimitExceeded, nil, "assistant workspace growth rate limit exceeded")
	}

	result, err := s.core.GrowRuntimeWorkspace(ctx, projectID, principal.AssistantID)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return oops.E(oops.CodeNotFound, err, "active assistant runtime not found")
		case errors.Is(err, ErrWorkspaceGrowthUnsupported):
			return oops.E(oops.CodeConflict, err, "assistant runtime workspace cannot be expanded")
		default:
			return oops.E(oops.CodeUnexpected, err, "expand assistant runtime workspace").LogError(ctx, s.logger,
				attr.SlogProjectID(projectID.String()),
				attr.SlogAssistantID(principal.AssistantID.String()),
			)
		}
	}

	payload, err := json.Marshal(result)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "encode workspace growth response")
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(payload); err != nil {
		return fmt.Errorf("write workspace growth response: %w", err)
	}
	return nil
}
