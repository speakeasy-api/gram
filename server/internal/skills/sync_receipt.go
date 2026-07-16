package skills

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/skills/repo"
)

type SyncReceiptStatus string

const (
	SyncReceiptStatusApplied         SyncReceiptStatus = "applied"
	SyncReceiptStatusConflictSkipped SyncReceiptStatus = "conflict_skipped"
	SyncReceiptStatusFSReadonly      SyncReceiptStatus = "fs_readonly"
)

func UpsertSkillSyncReceipt(ctx context.Context, queries *repo.Queries, params repo.UpsertSkillSyncReceiptParams) (repo.SkillSyncReceipt, error) {
	switch SyncReceiptStatus(params.Status) {
	case SyncReceiptStatusApplied, SyncReceiptStatusConflictSkipped, SyncReceiptStatusFSReadonly:
	case "":
		return repo.SkillSyncReceipt{}, fmt.Errorf("skill sync receipt status is required")
	default:
		return repo.SkillSyncReceipt{}, fmt.Errorf("invalid skill sync receipt status %q", params.Status)
	}

	receipt, err := queries.UpsertSkillSyncReceipt(ctx, params)
	if err != nil {
		return repo.SkillSyncReceipt{}, fmt.Errorf("upsert skill sync receipt: %w", err)
	}

	return receipt, nil
}
