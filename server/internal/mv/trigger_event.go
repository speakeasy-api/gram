package mv

import (
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

// BuildTriggerEventView converts a trigger dispatch event row into the API
// response type.
func BuildTriggerEventView(row triggerrepo.ListTriggerEventsRow) *types.TriggerEvent {
	return &types.TriggerEvent{
		ID:                row.ID.String(),
		TriggerInstanceID: row.TriggerInstanceID.UUID.String(),
		Status:            row.Status,
		Attempts:          int(row.Attempts),
		LastError:         conv.FromPGText[string](row.LastError),
		ChatID:            conv.FromNullableUUID(row.ChatID),
		CreatedAt:         conv.FromPGTimestamptz(row.CreatedAt),
		ProcessedAt:       conv.PtrEmpty(conv.FromPGTimestamptz(row.ProcessedAt)),
	}
}
