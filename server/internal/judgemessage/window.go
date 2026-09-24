package judgemessage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// Window is ordered evidence for one target and at most four neighbors.
// TargetIndex refers to Messages, never to an external identifier.
type Window struct {
	// Messages preserves actor and tool attribution without exposing database IDs.
	Messages []Payload `json:"messages"`

	// TargetIndex identifies the only event the judge should classify.
	TargetIndex int `json:"target_index"`
}

// WindowLoader retrieves conversation evidence only after a prefilter escalates.
type WindowLoader struct{ db repo.DBTX }

func NewWindowLoader(db repo.DBTX) *WindowLoader { return &WindowLoader{db: db} }

func (l *WindowLoader) Load(ctx context.Context, orgID, projectID string, target Message) (Window, error) {
	window := Window{Messages: []Payload{RenderPayload(target)}, TargetIndex: 0}
	// Unpersisted calls have no trustworthy position in a conversation.
	if target.AnchorID == uuid.Nil && target.ChatID == uuid.Nil {
		return window, nil
	}
	project, err := uuid.Parse(projectID)
	if err != nil {
		return Window{Messages: nil, TargetIndex: 0}, fmt.Errorf("parse judge context project: %w", err)
	}
	rows, err := repo.New(l.db).GetJudgeMessageWindow(ctx, repo.GetJudgeMessageWindowParams{
		AnchorID: target.AnchorID, ChatID: target.ChatID, ProjectID: project, OrganizationID: orgID,
	})
	if err != nil {
		return Window{Messages: nil, TargetIndex: 0}, fmt.Errorf("load judge context: %w", err)
	}
	return windowFromRows(target, rows)
}

func windowFromRows(target Message, rows []repo.GetJudgeMessageWindowRow) (Window, error) {
	window := Window{Messages: make([]Payload, 0, len(rows)), TargetIndex: -1}
	if len(rows) > 5 {
		return window, errors.New("judge context exceeds five messages")
	}
	for _, row := range rows {
		if row.ID == target.AnchorID {
			window.TargetIndex = len(window.Messages)
			window.Messages = append(window.Messages, RenderPayload(target))
			continue
		}
		kind := row.Role
		switch row.Role {
		case "tool":
			kind = message.ToolResponse
		case "assistant":
			kind = message.Assistant
		case "user":
			kind = message.User
		}
		msg := New(kind, "", row.Content)
		var calls []struct {
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		}
		if err := json.Unmarshal([]byte(row.ToolCalls), &calls); err != nil {
			return window, errors.New("invalid or oversized judge context tool calls")
		}
		if len(calls) > 0 {
			msg.Type = message.ToolRequest
			for _, call := range calls {
				msg.ToolCalls = append(msg.ToolCalls, NewToolCall(call.Function.Name, call.Function.Arguments))
			}
		}
		payload := RenderPayload(msg)
		if payload.ProducedBy == "unknown" {
			payload.ProducedBy = row.Role
		}
		// SQL retains one extra rune to report truncation explicitly.
		payload.Body, payload.BodyTruncated = truncatePayloadBody(msg.Body, MaxTrajectoryBodyRunes)
		payload.Decoded = decodedView(payload.Body)
		window.Messages = append(window.Messages, payload)
	}
	if target.AnchorID == uuid.Nil && len(rows) <= 4 {
		window.TargetIndex = len(window.Messages)
		window.Messages = append(window.Messages, RenderPayload(target))
	}
	if window.TargetIndex < 0 {
		return window, errors.New("judge context anchor is unavailable")
	}
	return window, nil
}
