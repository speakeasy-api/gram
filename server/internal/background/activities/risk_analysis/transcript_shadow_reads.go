package risk_analysis

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// batchContextMaxRunes mirrors GetMessageContentBatch's LEFT(prev.content,
// 4000): the prior user request and recent tool result each carry at most this
// many characters, whether they come from plaintext or an encrypted shadow.
const batchContextMaxRunes = 4000

// WithTranscriptShadowReads makes batch scans for the listed projects read
// message bodies from encrypted transcript shadows instead of plaintext. A nil
// store or an empty list leaves every project on plaintext.
func (a *AnalyzeBatch) WithTranscriptShadowReads(store *transcriptstore.Store, projectIDs ...uuid.UUID) *AnalyzeBatch {
	a.transcriptShadow = store
	a.transcriptShadowReadProjects = make(map[uuid.UUID]struct{}, len(projectIDs))
	for _, projectID := range projectIDs {
		a.transcriptShadowReadProjects[projectID] = struct{}{}
	}
	return a
}

// openShadowRows replaces the bodies a batch scans, and the context messages
// each row carries, with their encrypted shadows when the project reads through
// shadows. One LoadMany call opens the whole batch. A message with no shadow
// keeps its plaintext, so messages written before capture began still scan. A
// stale or unopenable shadow fails the batch: the activity retries and the
// batch is never marked analyzed from plaintext that a broken shadow hides.
func (a *AnalyzeBatch) openShadowRows(ctx context.Context, organizationID string, projectID uuid.UUID, rows []repo.GetMessageContentBatchRow) error {
	if a.transcriptShadow == nil || len(rows) == 0 {
		return nil
	}
	if _, enabled := a.transcriptShadowReadProjects[projectID]; !enabled {
		return nil
	}

	// A user request is often the context of several batch rows; open it once
	// so the read metric counts each message once.
	seen := make(map[uuid.UUID]struct{}, len(rows))
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		for _, id := range []uuid.UUID{row.ID, row.PriorUserRequestID, row.RecentUntrustedContentID} {
			if id == uuid.Nil {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}

	opened, err := a.transcriptShadow.LoadMany(ctx, organizationID, projectID, ids)
	if err != nil {
		return fmt.Errorf("open encrypted chat transcript: %w", err)
	}
	for i := range rows {
		row := &rows[i]
		if message, ok := opened[row.ID]; ok {
			row.Content = message.Content
			row.ToolCalls = message.ToolCalls
		}
		if message, ok := opened[row.PriorUserRequestID]; ok {
			row.PriorUserRequest = truncateRunes(message.Content, batchContextMaxRunes)
		}
		if message, ok := opened[row.RecentUntrustedContentID]; ok {
			row.RecentUntrustedContent = truncateRunes(message.Content, batchContextMaxRunes)
		}
	}
	return nil
}

// truncateRunes returns the first n characters of s, like PostgreSQL's LEFT.
func truncateRunes(s string, n int) string {
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}
