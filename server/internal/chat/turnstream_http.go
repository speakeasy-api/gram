package chat

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// HandleTurnStream serves a chat's turn frames as SSE. The dashboard opens one
// of these per turn and renders from it instead of polling chat.load.
//
// `after` is the cursor of the last frame the client applied. On a reconnect it
// is what makes the stream lossless: the handler replays everything published
// since that cursor before tailing. Omitted, the client gets the retained
// history from the start, which is what joining a turn late wants.
//
// Frames are `data: {...}` objects carrying their own `cursor`. The stream ends
// after a terminal frame, or when the client disconnects.
func (s *Service) HandleTurnStream(w http.ResponseWriter, r *http.Request) error {
	// These raw /chat routes get no auth middleware — HandleCompletion
	// authorizes itself and so must this, or every browser subscription is
	// rejected before the handler runs.
	ctx, authCtx, _, err := s.directAuthorize(r.Context(), r)
	if err != nil {
		return err
	}
	if authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{
		Scope:        authz.ScopeProjectRead,
		ResourceKind: "",
		ResourceID:   authCtx.ProjectID.String(),
		Dimensions:   nil,
	}); err != nil {
		return err
	}
	if s.turnStream == nil {
		return oops.E(oops.CodeInvalid, nil, "assistant turn streaming is not enabled")
	}

	chatID, parseErr := uuid.Parse(r.URL.Query().Get("chat_id"))
	if parseErr != nil {
		return oops.E(oops.CodeBadRequest, parseErr, "invalid chat_id")
	}
	// Watching a chat streams its content, so it is authorized exactly like
	// reading the transcript. Project scoping alone is not enough: it would let
	// an embedded chat-session token watch any other user's chat in the
	// project. This route keeps its own lookup rather than using
	// loadAuthorizedChat so every failure reads as "not found" — it is reachable
	// via CORS, and a distinct unauthorized response would be an existence
	// oracle.
	chat, err := s.repo.GetChat(ctx, repo.GetChatParams{ID: chatID, ProjectID: *authCtx.ProjectID})
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "chat not found")
	}
	if err := s.authorizeChatRead(ctx, authCtx, chat); err != nil {
		return err
	}

	frames, err := s.turnStream.Subscribe(ctx, chatID, r.URL.Query().Get("after"))
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "subscribe to turn frames").LogError(ctx, s.logger)
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, canFlush := w.(http.Flusher)
	if canFlush {
		flusher.Flush()
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case frame, open := <-frames:
			if !open {
				return nil
			}
			payload, err := json.Marshal(frame)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
				// Client hung up. The turn is unaffected, and whatever it
				// missed is replayable from its last cursor.
				return nil
			}
			if canFlush {
				flusher.Flush()
			}
			if frame.Kind == TurnFrameDone {
				return nil
			}
		}
	}
}
