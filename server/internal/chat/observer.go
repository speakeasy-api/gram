package chat

import (
	"context"

	"github.com/google/uuid"
)

// MessageObserver is notified when new chat messages are stored.
// Implementations must not block; heavy work should be dispatched asynchronously.
type MessageObserver interface {
	OnMessagesStored(ctx context.Context, projectID uuid.UUID)
}
