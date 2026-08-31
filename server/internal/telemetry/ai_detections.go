package telemetry

import (
	"context"
	"time"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// AIDetection is one AI-tool detection observed by a device-agent scan.
type AIDetection struct {
	OrganizationID string
	TargetID       string
	DeviceSerial   string
	UserEmail      string
	Signal         string
	Category       string
	Version        string
	SeenAt         time.Time
}

// AIScanReceipt records that a device ran an AI scan, match or not.
type AIScanReceipt struct {
	OrganizationID    string
	DeviceSerial      string
	UserEmail         string
	ScanStartedAt     time.Time
	ScanCompletedAt   time.Time
	TargetListVersion int32
	MatchCount        uint32
	ReceivedAt        time.Time
}

// UpsertAIDetections merges AI-scan detections into the ai_detections
// inventory, preserving first_seen via the repo's read-merge-write.
func (l *Logger) UpsertAIDetections(ctx context.Context, detections []AIDetection) error {
	if len(detections) == 0 || l.chConn == nil {
		return nil
	}

	now := time.Now()
	params := make([]repo.UpsertAIDetectionParams, 0, len(detections))
	for _, detection := range detections {
		if detection.OrganizationID == "" || detection.TargetID == "" || detection.UserEmail == "" || detection.Signal == "" {
			continue
		}
		seenAt := detection.SeenAt
		if seenAt.IsZero() {
			seenAt = now
		}
		params = append(params, repo.UpsertAIDetectionParams{
			OrganizationID: detection.OrganizationID,
			TargetID:       detection.TargetID,
			DeviceSerial:   detection.DeviceSerial,
			UserEmail:      detection.UserEmail,
			Signal:         detection.Signal,
			Category:       detection.Category,
			Version:        detection.Version,
			SeenAt:         seenAt,
			UpdatedAt:      now,
		})
	}

	if len(params) == 0 {
		return nil
	}

	if err := repo.New(l.chConn).UpsertAIDetections(l.detachedWriteContext(ctx), params); err != nil {
		return oops.E(oops.CodeUnexpected, err, "upsert ai detections")
	}

	return nil
}

// InsertAIScanReceipt appends one scan receipt to ai_scan_receipts.
func (l *Logger) InsertAIScanReceipt(ctx context.Context, receipt AIScanReceipt) error {
	if l.chConn == nil {
		return nil
	}
	if receipt.OrganizationID == "" {
		return nil
	}
	if receipt.ReceivedAt.IsZero() {
		receipt.ReceivedAt = time.Now()
	}

	err := repo.New(l.chConn).InsertAIScanReceipts(l.detachedWriteContext(ctx), []repo.InsertAIScanReceiptParams{{
		OrganizationID:    receipt.OrganizationID,
		DeviceSerial:      receipt.DeviceSerial,
		UserEmail:         receipt.UserEmail,
		ScanStartedAt:     receipt.ScanStartedAt,
		ScanCompletedAt:   receipt.ScanCompletedAt,
		TargetListVersion: receipt.TargetListVersion,
		MatchCount:        receipt.MatchCount,
		ReceivedAt:        receipt.ReceivedAt,
	}})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "insert ai scan receipt")
	}

	return nil
}
