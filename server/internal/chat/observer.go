package chat

import (
	"context"

	"github.com/google/uuid"
)

// MessageObserver is notified when new chat messages are stored. The
// messageIDs slice contains every ID just persisted, in insertion order, so
// observers can route per-message work without re-querying the database.
// Implementations must not block; heavy work should be dispatched asynchronously.
type MessageObserver interface {
	OnMessagesStored(ctx context.Context, projectID uuid.UUID, messageIDs []uuid.UUID)
}
