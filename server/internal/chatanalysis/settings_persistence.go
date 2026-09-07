package chatanalysis

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/businessmemory"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const MaxDailyCap = 10000

// Settings is the complete organization-level chat analysis settings view.
// Missing rows are represented as disabled with a zero cap.
type Settings struct {
	OrganizationID         string `json:"organization_id"`
	WorkUnitsEnabled       bool   `json:"work_units_enabled"`
	WorkUnitsDailyCap      int    `json:"work_units_daily_cap"`
	BusinessMemoryEnabled  bool   `json:"business_memory_enabled"`
	BusinessMemoryDailyCap int    `json:"business_memory_daily_cap"`
	IsDefault              bool   `json:"is_default"`
}

func LoadSettings(ctx context.Context, db *pgxpool.Pool, organizationID string) (Settings, error) {
	return loadSettings(ctx, repo.New(db), organizationID)
}

// UpsertSettings preserves the budget-lock, audit/outbox, and reload ordering
// shared by every administrative chat analysis settings surface.
func UpsertSettings(
	ctx context.Context,
	db *pgxpool.Pool,
	auditLogger *audit.Logger,
	organizationID string,
	judge string,
	enabled bool,
	dailyCap int,
	actor urn.Principal,
	actorDisplayName *string,
) (Settings, error) {
	if dailyCap < 0 || dailyCap > MaxDailyCap {
		return Settings{}, fmt.Errorf("daily cap must be between 0 and %d", MaxDailyCap)
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return Settings{}, fmt.Errorf("begin chat analysis settings upsert: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	queries := repo.New(tx)
	if err := queries.LockOrganizationChatAnalysisBudget(ctx, organizationID); err != nil {
		return Settings{}, fmt.Errorf("lock chat analysis settings: %w", err)
	}

	var beforeSnapshot *audit.ChatAnalysisSettingsSnapshot
	before, err := queries.GetChatAnalysisSettingForOrganizationJudge(ctx, repo.GetChatAnalysisSettingForOrganizationJudgeParams{
		OrganizationID: organizationID, Judge: judge,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return Settings{}, fmt.Errorf("get existing chat analysis settings: %w", err)
	default:
		snapshot := buildSnapshot(before)
		beforeSnapshot = &snapshot
	}

	row, err := queries.UpsertChatAnalysisSettingForOrganizationJudge(ctx, repo.UpsertChatAnalysisSettingForOrganizationJudgeParams{
		OrganizationID: organizationID, Judge: judge, Enabled: enabled, DailyCap: conv.SafeInt32(dailyCap),
	})
	if err != nil {
		return Settings{}, fmt.Errorf("upsert chat analysis settings: %w", err)
	}
	afterSnapshot := buildSnapshot(row)
	if err := auditLogger.LogChatAnalysisSettingsUpsert(ctx, tx, audit.LogChatAnalysisSettingsUpsertEvent{
		OrganizationID: organizationID, Actor: actor, ActorDisplayName: actorDisplayName, ActorSlug: nil,
		ChatAnalysisSettingsURN:            urn.NewChatAnalysisSettings(organizationID),
		ChatAnalysisSettingsSnapshotBefore: beforeSnapshot, ChatAnalysisSettingsSnapshotAfter: &afterSnapshot,
	}); err != nil {
		return Settings{}, fmt.Errorf("log chat analysis settings upsert: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Settings{}, fmt.Errorf("commit chat analysis settings upsert: %w", err)
	}

	settings, err := LoadSettings(ctx, db, organizationID)
	if err != nil {
		return Settings{}, fmt.Errorf("reload chat analysis settings: %w", err)
	}
	return settings, nil
}

func loadSettings(ctx context.Context, queries *repo.Queries, organizationID string) (Settings, error) {
	view := Settings{
		OrganizationID: organizationID, IsDefault: true,
		WorkUnitsEnabled: false, WorkUnitsDailyCap: 0,
		BusinessMemoryEnabled: false, BusinessMemoryDailyCap: 0,
	}
	settings := []struct {
		judge string
		apply func(repo.ChatAnalysisSetting)
	}{
		{judge: analysis.WorkUnitsJudgeName, apply: func(row repo.ChatAnalysisSetting) {
			view.WorkUnitsEnabled = row.Enabled
			view.WorkUnitsDailyCap = int(row.DailyCap)
		}},
		{judge: businessmemory.JudgeName, apply: func(row repo.ChatAnalysisSetting) {
			view.BusinessMemoryEnabled = row.Enabled
			view.BusinessMemoryDailyCap = int(row.DailyCap)
		}},
	}
	for _, setting := range settings {
		row, err := queries.GetChatAnalysisSettingForOrganizationJudge(ctx, repo.GetChatAnalysisSettingForOrganizationJudgeParams{
			OrganizationID: organizationID, Judge: setting.judge,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			continue
		case err != nil:
			return Settings{}, fmt.Errorf("get %s chat analysis setting: %w", setting.judge, err)
		default:
			view.IsDefault = false
			setting.apply(row)
		}
	}
	return view, nil
}

func buildSnapshot(row repo.ChatAnalysisSetting) audit.ChatAnalysisSettingsSnapshot {
	return audit.ChatAnalysisSettingsSnapshot{Judge: row.Judge, Enabled: row.Enabled, DailyCap: row.DailyCap}
}
