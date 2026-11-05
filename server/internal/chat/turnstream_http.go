package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/httpstream"
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
// after a terminal frame, when the client disconnects, or when it reaches the
// service's maximum stream age, in which case it ends with an SSE retry field
// and the client reconnects from its cursor.
func (s *Service) HandleTurnStream(w http.ResponseWriter, r *http.Request) error {
	// The load balancer's response deadline runs from its receipt of the
	// request, so the stream's age is measured from here, not from its first
	// frame.
	started := time.Now()

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
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	heartbeat := time.NewTicker(httpstream.HeartbeatInterval)
	defer heartbeat.Stop()
	var maxAge <-chan time.Time
	if s.streamMaxAge > 0 {
		timer := time.NewTimer(time.Until(started.Add(s.streamMaxAge)))
		defer timer.Stop()
		maxAge = timer.C
	}

	reason := streamTurnFrames(ctx, w, frames, heartbeat.C, maxAge)
	s.streamMetrics.RecordEnd(ctx, r, reason, started, time.Now())
	return nil
}

// streamTurnFrames writes frames to w as SSE events until the turn ends, the
// client goes away, or the stream reaches its maximum age, and reports why it
// stopped. Every heartbeat tick writes an SSE comment so a turn that is quiet
// while a tool runs does not look idle to the client or anything between. A
// tick on maxAge ends the stream with an SSE retry field; a nil maxAge never
// ticks. The response headers must already be written.
func streamTurnFrames(ctx context.Context, w http.ResponseWriter, frames <-chan TurnFrame, heartbeat <-chan time.Time, maxAge <-chan time.Time) httpstream.EndReason {
	flusher, canFlush := w.(http.Flusher)
	flush := func() {
		if canFlush {
			flusher.Flush()
		}
	}

	for {
		select {
		case <-ctx.Done():
			return httpstream.CanceledReason(ctx)
		case <-heartbeat:
			if _, err := w.Write(httpstream.Heartbeat); err != nil {
				return httpstream.CanceledReason(ctx)
			}
			flush()
		case <-maxAge:
			// Whether or not the retry field lands, the stream is over; the
			// client reconnects from its cursor either way.
			_, _ = w.Write(httpstream.RetryField(httpstream.RetryDelay()))
			flush()
			return httpstream.EndMaxAge
		case frame, open := <-frames:
			if !open {
				// The subscription closes on its own only when the client
				// went away or the frame source failed.
				if ctx.Err() != nil {
					return httpstream.CanceledReason(ctx)
				}
				return httpstream.EndError
			}
			payload, err := json.Marshal(frame)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
				// Client hung up. The turn is unaffected, and whatever it
				// missed is replayable from its last cursor.
				return httpstream.CanceledReason(ctx)
			}
			flush()
			if frame.Kind == TurnFrameDone {
				return httpstream.EndCompleted
			}
		}
	}
}
