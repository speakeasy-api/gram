package risk_analysis

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

type BatchMessage = batchMessage

const (
	InlineBatchExecutionPath  = inlineBatchExecutionPath
	ShadowStreamExecutionPath = shadowStreamExecutionPath
)

func NewTestBatchMessage(typ message.Type) BatchMessage { return msg(typ) }

func NewTestToolRequest(names ...string) BatchMessage { return toolReq(names...) }

func (m batchMessage) ScanSurface() string { return m.scanSurface() }

func BatchScanRequestID(args AnalyzeBatchArgs, discriminator string) uuid.UUID {
	return batchScanRequestID(args, discriminator)
}

func (a *AnalyzeBatch) RecordBatchResults(ctx context.Context, definition metering.Definition, args AnalyzeBatchArgs, messages []BatchMessage, results []scanners.Result, startedAt time.Time) {
	a.recordBatchResults(ctx, definition, args, messages, results, startedAt)
}

func (a *AnalyzeBatch) PublishPresidioScanRequests(ctx context.Context, args AnalyzeBatchArgs, messages []BatchMessage, scoreThreshold float64) error {
	return a.publishPresidioScanRequests(ctx, args, messages, scoreThreshold)
}
