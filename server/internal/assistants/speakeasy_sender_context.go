package assistants

import (
	"context"
	"encoding/json"
	"fmt"

	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// speakeasySenderContextLabel prefixes the trusted sender line in a turn's
// message-context block.
const speakeasySenderContextLabel = "SpeakeasySender (verified Speakeasy account linked to the Slack sender, JSON): "

// speakeasySenderContext is the trusted sender identity shown to the
// assistant. Values come from the Speakeasy users table, never the Slack
// payload.
type speakeasySenderContext struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
}

type speakeasyUserLookup func(ctx context.Context, id string) (usersrepo.User, error)

// renderSpeakeasySenderContext returns the message-context line describing
// the linked Speakeasy user behind a Speakeasy Slack app turn, or "" when the
// turn has no verified human sender. The owner fallback is never described.
// Values are JSON encoded so user-controlled text cannot break out of the line
// or the message-context block.
func renderSpeakeasySenderContext(ctx context.Context, source string, identity turnIdentity, lookup speakeasyUserLookup) (string, error) {
	if source != sourceKindSpeakeasySlack || !identity.HumanKnown || identity.UserID == "" {
		return "", nil
	}
	user, err := lookup(ctx, identity.UserID)
	if err != nil {
		return "", fmt.Errorf("load Speakeasy Slack sender: %w", err)
	}
	if user.ID != identity.UserID {
		return "", fmt.Errorf("load Speakeasy Slack sender: user mismatch")
	}
	// json.Marshal escapes <, >, & and control characters, so the encoded
	// value is a single line without markup.
	encoded, err := json.Marshal(speakeasySenderContext{UserID: user.ID, DisplayName: user.DisplayName, Email: user.Email})
	if err != nil {
		return "", fmt.Errorf("encode Speakeasy Slack sender: %w", err)
	}
	return speakeasySenderContextLabel + string(encoded), nil
}

// speakeasySenderContextLine loads the turn's trusted sender context.
func (s *ServiceCore) speakeasySenderContextLine(ctx context.Context, thread assistantThreadRecord, identity turnIdentity) (string, error) {
	return renderSpeakeasySenderContext(ctx, thread.SourceKind, identity, usersrepo.New(s.db).GetUser)
}
