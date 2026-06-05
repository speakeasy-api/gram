package assistants

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// recordCompactedGenerationMaxBodyBytes caps the request body to a generous
// ceiling for a compacted transcript. A summary plus a handful of preserved
// turns should fit comfortably; an oversized body almost certainly signals
// the runner is misusing the endpoint to dump a full transcript.
const recordCompactedGenerationMaxBodyBytes = 1 * 1024 * 1024

type recordCompactedGenerationRequest struct {
	ThreadID string           `json:"thread_id"`
	Messages []runtimeMessage `json:"messages"`
}

func (s *Service) handleRecordCompactedGeneration(w http.ResponseWriter, r *http.Request) error {
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

	body, err := io.ReadAll(io.LimitReader(r.Body, recordCompactedGenerationMaxBodyBytes))
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "read compaction request")
	}
	var req recordCompactedGenerationRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return oops.E(oops.CodeBadRequest, err, "decode compaction request")
	}
	threadID, err := uuid.Parse(req.ThreadID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid thread_id")
	}

	// Per-thread token (ThreadID claim populated) may only persist its own
	// thread's compaction; assistant-only tokens (ThreadID zero) still flow
	// through.
	if principal.ThreadID != uuid.Nil && principal.ThreadID != threadID {
		return oops.E(oops.CodeForbidden, nil, "token thread does not match requested thread")
	}

	if err := s.core.RecordCompactedGeneration(ctx, projectID, threadID, principal.AssistantID, req.Messages); err != nil {
		return err
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}
