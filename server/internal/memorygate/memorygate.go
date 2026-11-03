// Package memorygate decides whether Organizational Memory is on for an
// organization.
//
// The feature has no rollout flag of its own. It follows the staff admin
// "Business Memory Extraction" toggle (the chat analysis setting for the
// business_memory judge), so one switch turns on fact extraction, masked
// session summaries, digest rollups and the team memory Platform MCP tools
// together.
package memorygate

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	analysisrepo "github.com/speakeasy-api/gram/server/internal/chat/analysis/repo"
)

// JudgeName is the chat analysis judge whose organization setting gates
// Organizational Memory. It must equal businessmemory.JudgeName; a test
// enforces that without importing businessmemory here, which keeps this
// package a leaf that any layer can depend on.
const JudgeName = "business_memory"

// Enabled reports whether Organizational Memory is on for the organization.
// An organization with no setting row is off. A lookup error is returned so
// callers can fail closed or retry.
func Enabled(ctx context.Context, db analysisrepo.DBTX, organizationID string) (bool, error) {
	setting, err := analysisrepo.New(db).GetChatAnalysisSettingForOrganizationJudge(ctx, analysisrepo.GetChatAnalysisSettingForOrganizationJudgeParams{
		OrganizationID: organizationID,
		Judge:          JudgeName,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read organizational memory setting: %w", err)
	}
	return setting.Enabled, nil
}

// Set turns Organizational Memory on or off for the organization, keeping
// any daily cap already configured. It exists for tests, benchmarks and
// seeds; production enablement goes through the staff admin dashboard.
func Set(ctx context.Context, db analysisrepo.DBTX, organizationID string, enabled bool) error {
	queries := analysisrepo.New(db)
	var dailyCap int32
	existing, err := queries.GetChatAnalysisSettingForOrganizationJudge(ctx, analysisrepo.GetChatAnalysisSettingForOrganizationJudgeParams{
		OrganizationID: organizationID,
		Judge:          JudgeName,
	})
	switch {
	case err == nil:
		dailyCap = existing.DailyCap
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("read organizational memory setting: %w", err)
	}
	if _, err := queries.UpsertChatAnalysisSettingForOrganizationJudge(ctx, analysisrepo.UpsertChatAnalysisSettingForOrganizationJudgeParams{
		OrganizationID: organizationID,
		Judge:          JudgeName,
		Enabled:        enabled,
		DailyCap:       dailyCap,
	}); err != nil {
		return fmt.Errorf("set organizational memory setting: %w", err)
	}
	return nil
}
