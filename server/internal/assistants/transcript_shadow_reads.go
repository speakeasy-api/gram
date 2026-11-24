package assistants

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
)

// SetTranscriptShadowReads makes assistant history for the listed projects
// read message bodies from encrypted shadows instead of plaintext, as chat
// reads do on the server. An empty list or a nil store leaves every project on
// plaintext.
func (s *ServiceCore) SetTranscriptShadowReads(store *transcriptstore.Store, projectIDs ...uuid.UUID) {
	s.transcriptShadow = store
	s.shadowReadProjects = make(map[uuid.UUID]struct{}, len(projectIDs))
	for _, projectID := range projectIDs {
		s.shadowReadProjects[projectID] = struct{}{}
	}
}

func (s *ServiceCore) readsTranscriptShadows(projectID uuid.UUID) bool {
	if s.transcriptShadow == nil {
		return false
	}
	_, enabled := s.shadowReadProjects[projectID]
	return enabled
}

// openHistoryTranscriptShadows resolves the project's organization only for
// projects that read through shadows, so every other project's history load
// costs no extra query.
func (s *ServiceCore) openHistoryTranscriptShadows(ctx context.Context, projectID uuid.UUID, messages []chatrepo.ChatMessage) error {
	if !s.readsTranscriptShadows(projectID) || len(messages) == 0 {
		return nil
	}
	organizationID, err := chatrepo.New(s.db).GetProjectOrganizationID(ctx, projectID)
	if err != nil {
		return fmt.Errorf("resolve assistant chat organization: %w", err)
	}
	return s.openTranscriptShadows(ctx, organizationID, projectID, messages)
}

// openTranscriptShadows replaces message bodies with their encrypted shadows
// when the project reads through shadows, and leaves them untouched for every
// other project. A message with no shadow keeps its plaintext body, so threads
// written before capture began still replay. A stale or unopenable shadow
// fails the read: it means capture or the data is broken, and plaintext must
// not paper over that.
func (s *ServiceCore) openTranscriptShadows(ctx context.Context, organizationID string, projectID uuid.UUID, messages []chatrepo.ChatMessage) error {
	if !s.readsTranscriptShadows(projectID) || len(messages) == 0 {
		return nil
	}

	ids := make([]uuid.UUID, len(messages))
	for i, msg := range messages {
		ids[i] = msg.ID
	}
	opened, err := s.transcriptShadow.LoadMany(ctx, organizationID, projectID, ids)
	if err != nil {
		return fmt.Errorf("open encrypted chat transcript: %w", err)
	}
	for i := range messages {
		message, ok := opened[messages[i].ID]
		if !ok {
			continue
		}
		messages[i].Content = message.Content
		messages[i].ContentRaw = message.ContentRaw
		messages[i].ToolCalls = message.ToolCalls
	}
	return nil
}
