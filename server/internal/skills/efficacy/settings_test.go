package efficacy

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/skills/repo"
)

func TestEffectiveSettingsFallBackToDefaultsWhenNoRow(t *testing.T) {
	t.Parallel()

	settings := Effective(repo.GetSkillEfficacySettingsForProjectRow{
		OrganizationID:   "org",
		Enabled:          pgtype.Bool{Bool: false, Valid: false},
		PerSkillDailyCap: pgtype.Int4{Int32: 0, Valid: false},
		OrgDailyCap:      pgtype.Int4{Int32: 0, Valid: false},
		NewVersionBurst:  pgtype.Int4{Int32: 0, Valid: false},
	})

	require.Equal(t, Settings{
		Enabled:          true,
		PerSkillDailyCap: 10,
		OrgDailyCap:      100,
		NewVersionBurst:  25,
	}, settings)
}

func TestEffectiveSettingsUseStoredRow(t *testing.T) {
	t.Parallel()

	settings := Effective(repo.GetSkillEfficacySettingsForProjectRow{
		OrganizationID:   "org",
		Enabled:          pgtype.Bool{Bool: false, Valid: true},
		PerSkillDailyCap: pgtype.Int4{Int32: 0, Valid: true},
		OrgDailyCap:      pgtype.Int4{Int32: 7, Valid: true},
		NewVersionBurst:  pgtype.Int4{Int32: 3, Valid: true},
	})

	require.Equal(t, Settings{
		Enabled:          false,
		PerSkillDailyCap: 0,
		OrgDailyCap:      7,
		NewVersionBurst:  3,
	}, settings)
}

func TestSettingsAdmitWork(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		settings Settings
		want     bool
	}{
		{"defaults", Settings{Enabled: true, PerSkillDailyCap: 10, OrgDailyCap: 100, NewVersionBurst: 25}, true},
		{"disabled", Settings{Enabled: false, PerSkillDailyCap: 10, OrgDailyCap: 100, NewVersionBurst: 25}, false},
		{"zero org cap", Settings{Enabled: true, PerSkillDailyCap: 10, OrgDailyCap: 0, NewVersionBurst: 25}, false},
		{"daily only", Settings{Enabled: true, PerSkillDailyCap: 10, OrgDailyCap: 100, NewVersionBurst: 0}, true},
		{"burst only", Settings{Enabled: true, PerSkillDailyCap: 0, OrgDailyCap: 100, NewVersionBurst: 25}, true},
		{"no skill capacity", Settings{Enabled: true, PerSkillDailyCap: 0, OrgDailyCap: 100, NewVersionBurst: 0}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, test.settings.admitsWork())
		})
	}
}
