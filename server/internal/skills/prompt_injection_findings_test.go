package skills_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

func TestGetSkillReturnsPromptInjectionFindingsForCurrentVersion(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "risky-skill", "First version.")
	policyID := uuid.New()
	_, err := riskrepo.New(ti.conn).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
		ID: policyID, ProjectID: ti.projectID, OrganizationID: ti.authContext.ActiveOrganizationID,
		Name: "Prompt injection", Sources: []string{"prompt_injection"}, Enabled: true,
		Action: "flag", AudienceType: "everyone", AutoName: false,
	})
	require.NoError(t, err)

	firstVersionID := uuid.MustParse(created.Version.ID)
	err = riskrepo.New(ti.conn).RecordSkillPromptInjectionScan(ctx, riskrepo.RecordSkillPromptInjectionScanParams{
		SkillVersionID: uuid.NullUUID{UUID: firstVersionID, Valid: true},
		ProjectID:      ti.projectID, Source: "prompt_injection", Found: true,
		RuleID:      pgtype.Text{String: "prompt_injection", Valid: true},
		Description: pgtype.Text{String: "Attempts to override trusted instructions.", Valid: true},
		Match:       pgtype.Text{String: "raw manifest must not be returned", Valid: true},
		Confidence:  pgtype.Float8{Float64: 0.98, Valid: true},
	})
	require.NoError(t, err)

	got, err := ti.service.Get(ctx, &gen.GetPayload{ID: created.Skill.ID})
	require.NoError(t, err)
	require.Equal(t, created.Version.ID, got.LatestVersion.ID)
	require.Equal(t, []*gen.SkillPromptInjectionFinding{{
		RuleID: "prompt_injection", Description: "Attempts to override trusted instructions.", Confidence: 0.98,
	}}, got.PromptInjectionFindings)

	_, err = riskrepo.New(ti.conn).BumpRiskPolicyVersion(ctx, riskrepo.BumpRiskPolicyVersionParams{
		ID: policyID, ProjectID: ti.projectID,
	})
	require.NoError(t, err)
	got, err = ti.service.Get(ctx, &gen.GetPayload{ID: created.Skill.ID})
	require.NoError(t, err)
	require.Empty(t, got.PromptInjectionFindings)

	err = riskrepo.New(ti.conn).RecordSkillPromptInjectionScan(ctx, riskrepo.RecordSkillPromptInjectionScanParams{
		SkillVersionID: uuid.NullUUID{UUID: firstVersionID, Valid: true},
		ProjectID:      ti.projectID, Source: "prompt_injection", Found: true,
		RuleID:      pgtype.Text{String: "prompt_injection", Valid: true},
		Description: pgtype.Text{String: "Detected by the updated policy.", Valid: true},
		Match:       pgtype.Text{String: "updated raw match", Valid: true},
		Confidence:  pgtype.Float8{Float64: 0.99, Valid: true},
	})
	require.NoError(t, err)
	got, err = ti.service.Get(ctx, &gen.GetPayload{ID: created.Skill.ID})
	require.NoError(t, err)
	require.Equal(t, []*gen.SkillPromptInjectionFinding{{
		RuleID: "prompt_injection", Description: "Detected by the updated policy.", Confidence: 0.99,
	}}, got.PromptInjectionFindings)

	second, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID:      created.Skill.ID,
		Content: skillManifest("risky-skill", "Second version.", "safe replacement"),
	})
	require.NoError(t, err)
	got, err = ti.service.Get(ctx, &gen.GetPayload{ID: created.Skill.ID})
	require.NoError(t, err)
	require.Equal(t, second.Version.ID, got.LatestVersion.ID)
	require.Empty(t, got.PromptInjectionFindings)

	_, err = ti.service.RestoreVersion(ctx, &gen.RestoreVersionPayload{
		ID: created.Skill.ID, VersionID: created.Version.ID,
	})
	require.NoError(t, err)
	got, err = ti.service.Get(ctx, &gen.GetPayload{ID: created.Skill.ID})
	require.NoError(t, err)
	require.Equal(t, created.Version.ID, got.LatestVersion.ID)
	require.Len(t, got.PromptInjectionFindings, 1)
}
