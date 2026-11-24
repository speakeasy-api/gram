package hooks

import (
	"context"
	"fmt"

	"github.com/google/uuid"

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

// openTranscriptShadows opens the shadows of the given messages with one
// LoadMany call, so a hook request decrypts a page of messages at once rather
// than one message at a time. A message with no shadow is left out of the
// result and the caller keeps its plaintext body, like chat.load. A stale or
// unauthentic shadow fails the read: plaintext must not paper over broken
// capture.
func (s *Service) openTranscriptShadows(ctx context.Context, organizationID string, projectID uuid.UUID, messageIDs []uuid.UUID) (map[uuid.UUID]transcriptcrypto.Message, error) {
	if len(messageIDs) == 0 {
		return map[uuid.UUID]transcriptcrypto.Message{}, nil
	}
	opened, err := s.transcriptShadow.LoadMany(ctx, organizationID, projectID, messageIDs)
	if err != nil {
		return nil, fmt.Errorf("open encrypted chat transcript: %w", err)
	}
	return opened, nil
}
