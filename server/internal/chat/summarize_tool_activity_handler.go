package chat

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

// summarizeToolActivityMaxBody bounds the request body. A turn is capped at 20
// tool calls, each with a <=256-char name and <=600-char arguments blob, plus a
// <=2000-char user prompt, so this leaves generous headroom while rejecting
// oversized payloads before decoding.
const summarizeToolActivityMaxBody = 64 * 1024

type toolActivityCallInput struct {
	Name      string  `json:"name"`
	Arguments *string `json:"arguments"`
}

type summarizeToolActivityRequest struct {
	ToolCalls   []toolActivityCallInput `json:"tool_calls"`
	UserMessage *string                 `json:"user_message"`
	InProgress  bool                    `json:"in_progress"`
}

type summarizeToolActivityResponse struct {
	Summary string `json:"summary"`
}

// handleSummarizeToolActivity is a raw (non-Goa) HTTP handler that turns a
// turn's tool calls into a short human-readable "task" label for the chat UI.
// It is deliberately not a Goa-designed method: the dashboard calls it directly
// and it must stay off the public SDK/OpenAPI surface. Auth mirrors the sibling
// completion handler (session or API key, plus project) via directAuthorize.
func (s *Service) handleSummarizeToolActivity(w http.ResponseWriter, r *http.Request) error {
	ctx, _, _, err := s.directAuthorize(r.Context(), r)
	if err != nil {
		return err
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, summarizeToolActivityMaxBody))
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "read tool activity request")
	}

	var req summarizeToolActivityRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return oops.E(oops.CodeBadRequest, err, "decode tool activity request")
	}

	summary, err := s.summarizeToolActivity(ctx, &req)
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(summarizeToolActivityResponse{Summary: summary}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "encode tool activity response").LogError(ctx, s.logger)
	}
	return nil
}
