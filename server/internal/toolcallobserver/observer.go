// Package toolcallobserver defines a narrow, content-free tool-success signal
// shared by runtime adapters and product-specific recorders.
package toolcallobserver

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// SuccessObservation identifies a successful tool call without retaining tool
// arguments, result content, headers, URLs, or session data.
type SuccessObservation struct {
	OrganizationID string
	UserID         string
	ProjectID      uuid.UUID
	MCPServerID    uuid.UUID
	ToolName       string
	SucceededAt    time.Time
}

// SuccessRecorder accepts best-effort observations. Implementations must never
// reject the caller's tool response.
type SuccessRecorder interface {
	RecordSuccessfulToolCall(context.Context, SuccessObservation)
}
