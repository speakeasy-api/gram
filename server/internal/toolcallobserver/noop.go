package toolcallobserver

import "context"

// NoopSuccessRecorder preserves the proxy's content-free observation seam for
// callers that do not compose Platform MCP selected-use tracking.
type NoopSuccessRecorder struct{}

func (NoopSuccessRecorder) RecordSuccessfulToolCall(context.Context, SuccessObservation) {}
