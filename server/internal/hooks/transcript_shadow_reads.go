package hooks

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/google/uuid"

	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptcrypto"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
)

// transcriptShadowReader opens the encrypted shadows of stored chat messages.
// transcriptstore.Store implements it; tests wrap the store to count reads.
type transcriptShadowReader interface {
	// LoadMany opens the shadows of the given messages. A message with no
	// shadow is left out of the result, and a stale or unauthentic shadow
	// fails the whole call.
	LoadMany(ctx context.Context, organizationID string, projectID uuid.UUID, messageIDs []uuid.UUID) (map[uuid.UUID]transcriptcrypto.Message, error)
}

// SetTranscriptShadowReads makes hook reconciliation for the listed projects
// read stored message bodies from encrypted shadows instead of plaintext. A
// nil store or an empty list leaves every project on plaintext.
func (s *Service) SetTranscriptShadowReads(store *transcriptstore.Store, projectIDs ...uuid.UUID) {
	s.transcriptShadow = nil
	s.transcriptShadowReadProjects = make(map[uuid.UUID]struct{}, len(projectIDs))
	if store == nil {
		return
	}
	s.transcriptShadow = store
	for _, projectID := range projectIDs {
		s.transcriptShadowReadProjects[projectID] = struct{}{}
	}
}

// readsTranscriptShadows reports whether the project's stored message bodies
// are read through encrypted shadows. It runs no query.
func (s *Service) readsTranscriptShadows(projectID uuid.UUID) bool {
	if s.transcriptShadow == nil {
		return false
	}
	_, enabled := s.transcriptShadowReadProjects[projectID]
	return enabled
}

// promptShadowPageSize bounds how many prompts one LoadMany call opens. An
// attachment almost always names a recent prompt, so the first page usually
// matches every hash and the read stops there.
const promptShadowPageSize = 16

// openPromptShadows opens the chat's user prompts from encrypted shadows,
// newest first and a page per LoadMany call, until every wanted hash has
// matched. It returns the body of each prompt it read; a prompt with no shadow
// keeps its plaintext body, like chat.load. A stale or unauthentic shadow
// fails the read: plaintext must not paper over broken capture. It uses its
// own pooled connections, so call it before taking a transaction.
func (s *Service) openPromptShadows(ctx context.Context, organizationID string, chatID, projectID uuid.UUID, wanted map[string]struct{}) (map[uuid.UUID]string, error) {
	candidates, err := chatRepo.New(s.db).ListClaudeUserMessagesForPromptAttachmentParent(ctx, chatRepo.ListClaudeUserMessagesForPromptAttachmentParentParams{
		ChatID:    chatID,
		ProjectID: projectID,
	})
	if err != nil {
		return nil, fmt.Errorf("list prompt attachment parent candidates: %w", err)
	}
	remaining := maps.Clone(wanted)
	bodies := make(map[uuid.UUID]string, min(len(candidates), promptShadowPageSize))
	for page := range slices.Chunk(candidates, promptShadowPageSize) {
		ids := make([]uuid.UUID, 0, len(page))
		for _, candidate := range page {
			ids = append(ids, candidate.ID)
		}
		opened, err := s.transcriptShadow.LoadMany(ctx, organizationID, projectID, ids)
		if err != nil {
			return nil, fmt.Errorf("open encrypted chat transcript: %w", err)
		}
		for _, candidate := range page {
			body := candidate.Content
			if message, ok := opened[candidate.ID]; ok {
				body = message.Content
			}
			bodies[candidate.ID] = body
			delete(remaining, promptBodyHash(body))
		}
		if len(remaining) == 0 {
			break
		}
	}
	return bodies, nil
}
