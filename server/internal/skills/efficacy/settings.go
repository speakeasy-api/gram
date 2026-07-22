package efficacy

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
)

// Budgets applied when an organization has no skill_efficacy_settings row.
const (
	DefaultEnabled          = true
	DefaultPerSkillDailyCap = 10
	DefaultOrgDailyCap      = 100
	DefaultNewVersionBurst  = 25
)

// Effective resolves the stored settings for a project's organization. The
// query LEFT JOINs the settings table, so an organization with no row comes
// back with null columns and every grain falls back to its default. Columns are
// resolved individually rather than all-or-nothing, so a partially populated
// row can never zero a cap it does not carry.
func Effective(row repo.GetSkillEfficacySettingsForProjectRow) Settings {
	settings := Settings{
		Enabled:          DefaultEnabled,
		PerSkillDailyCap: DefaultPerSkillDailyCap,
		OrgDailyCap:      DefaultOrgDailyCap,
		NewVersionBurst:  DefaultNewVersionBurst,
	}

	if row.Enabled.Valid {
		settings.Enabled = row.Enabled.Bool
	}
	if row.PerSkillDailyCap.Valid {
		settings.PerSkillDailyCap = row.PerSkillDailyCap.Int32
	}
	if row.OrgDailyCap.Valid {
		settings.OrgDailyCap = row.OrgDailyCap.Int32
	}
	if row.NewVersionBurst.Valid {
		settings.NewVersionBurst = row.NewVersionBurst.Int32
	}

	return settings
}

func (s Settings) admitsWork() bool {
	return s.Enabled && s.OrgDailyCap > 0 && (s.PerSkillDailyCap > 0 || s.NewVersionBurst > 0)
}

// admitsWork answers whether the project's organization may have efficacy work
// spent on it at all — the product entitlement, and the two settings that switch
// the pipeline off outright. A project that no longer resolves names no
// organization to bill and admits nothing.
//
// The reservation asks the same question of the settings it reads under its own
// budget lock, because that is where the caps it also needs come from. The
// enqueue asks it here so a queue is never built for an organization no
// reservation can ever spend for.
func admitsWork(ctx context.Context, queries *repo.Queries, features FeatureChecker, projectID uuid.UUID) (bool, error) {
	row, err := queries.GetSkillEfficacySettingsForProject(ctx, projectID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("read skill efficacy settings: %w", err)
	}

	entitled, err := features.IsFeatureEnabled(ctx, row.OrganizationID, productfeatures.FeatureSkills)
	if err != nil {
		return false, fmt.Errorf("check skills product feature: %w", err)
	}
	if !entitled {
		return false, nil
	}

	settings := Effective(row)

	return settings.admitsWork(), nil
}
