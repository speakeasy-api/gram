package assistantmemories

import (
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/memory/repo"
)

func memoryToView(m repo.GetAssistantMemoryByIDRow) *types.AssistantMemory {
	tags := m.Tags
	if tags == nil {
		tags = []string{}
	}

	createdAt := ""
	if m.CreatedAt.Valid {
		createdAt = m.CreatedAt.Time.UTC().Format(time.RFC3339Nano)
	}

	updatedAt := ""
	if m.UpdatedAt.Valid {
		updatedAt = m.UpdatedAt.Time.UTC().Format(time.RFC3339Nano)
	}

	lastAccess := ""
	if m.LastAccess.Valid {
		lastAccess = m.LastAccess.Time.UTC().Format(time.RFC3339Nano)
	}

	validAt := ""
	if m.ValidAt.Valid {
		validAt = m.ValidAt.Time.UTC().Format(time.RFC3339Nano)
	}

	var supersededAt *string
	if m.SupersededAt.Valid {
		s := m.SupersededAt.Time.UTC().Format(time.RFC3339Nano)
		supersededAt = &s
	}

	var deletedAt *string
	if m.DeletedAt.Valid {
		s := m.DeletedAt.Time.UTC().Format(time.RFC3339Nano)
		deletedAt = &s
	}

	var supersedesID *string
	if m.SupersedesID.Valid {
		s := m.SupersedesID.UUID.String()
		supersedesID = &s
	}

	return &types.AssistantMemory{
		ID:           m.ID.String(),
		AssistantID:  m.AssistantID.UUID.String(),
		Content:      m.Content,
		Tags:         tags,
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
		LastAccess:   lastAccess,
		ValidAt:      validAt,
		SupersededAt: supersededAt,
		DeletedAt:    deletedAt,
		SupersedesID: supersedesID,
	}
}
