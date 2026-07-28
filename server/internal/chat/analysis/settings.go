package analysis

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/chat/analysis/repo"
)

// DefaultJudgeDailyCap is the cap the settings surfaces suggest when switching
// a judge on for an organization that never stored one. The pipeline itself
// never falls back to it: a configured judge's row always stores its cap (the
// column is NOT NULL), and a judge with no row is off.
const DefaultJudgeDailyCap int32 = 100

// Settings are the effective per-organization budgets: the daily cap for each
// enabled judge. A judge absent from the map is off for the organization.
type Settings struct {
	OrganizationID string
	// JudgeDailyCaps holds one entry per enabled judge. A cap of 0 disables the
	// judge as surely as enabled=false does — the reservation can never admit
	// its units.
	JudgeDailyCaps map[string]int32
}

// settingsForProject resolves the organization's judge switches through the
// project, keeping only judges the roster actually runs: a settings row for a
// judge that was unregistered stays in the table but admits nothing. No rows at
// all means the project is gone or deleted, which callers treat the same as
// nothing enabled. There is no default enablement and no default cap: a judge
// with no chat_analysis_settings row is off, and a configured judge's row
// always stores its cap (the column is NOT NULL), so the pipeline spends
// nothing for an organization until someone opts a judge in.
func settingsForProject(ctx context.Context, queries *repo.Queries, judges *Judges, projectID uuid.UUID) (Settings, error) {
	rows, err := queries.GetChatAnalysisSettingsForProject(ctx, projectID)
	if err != nil {
		return Settings{}, fmt.Errorf("read chat analysis settings: %w", err)
	}

	settings := Settings{OrganizationID: "", JudgeDailyCaps: make(map[string]int32)}
	for _, row := range rows {
		settings.OrganizationID = row.OrganizationID
		if !row.Judge.Valid || !row.Enabled.Valid || !row.Enabled.Bool {
			continue
		}
		if _, ok := judges.Get(row.Judge.String); !ok {
			continue
		}
		// DailyCap is null only on the settings-less LEFT JOIN row, and that row's
		// null judge already continued above: a judge that reaches here always
		// carries its stored cap.
		settings.JudgeDailyCaps[row.Judge.String] = row.DailyCap.Int32
	}

	return settings, nil
}

// admitsWork answers whether any judge may have work spent on it at all.
func (s Settings) admitsWork() bool {
	for _, dailyCap := range s.JudgeDailyCaps {
		if dailyCap > 0 {
			return true
		}
	}
	return false
}

// enabledJudges lists the judges the settings admit, in no particular order.
func (s Settings) enabledJudges() []string {
	names := make([]string, 0, len(s.JudgeDailyCaps))
	for name, dailyCap := range s.JudgeDailyCaps {
		if dailyCap > 0 {
			names = append(names, name)
		}
	}
	return names
}
