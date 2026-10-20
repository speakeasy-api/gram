package demoseed

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
)

// skillContentRunbookV2 is the runbook's v2 content: v1 plus a paging step.
const skillContentRunbookV2 = skillContentRunbook + "- page the on-call before any manual failover.\n"

// skillsRunbookV2 is the runbook's second version, which the drift split and
// the recent runbook activations point at.
func (s *seeder) skillsRunbookV2() uuid.UUID { return s.fixed("0000-4000-a000-0000000052b3") }

// skillsManual is the per-skill data of the three manual skills, in
// skillNames order (index i-1 is skill i).
var skillsManual = []struct {
	displayName string
	summary     string
	content     string
}{
	{"Support Refunds", "How to process customer refunds safely.", skillContentRefunds},
	{"Triage Incident", "Steps for triaging a production incident.", skillContentTriage},
	{"Runbook", "General operational runbook for the Acme stack.", skillContentRunbook},
}

func skillsTS(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, InfinityModifier: pgtype.Finite, Valid: true}
}

// seedSkills: Prompts, skills, skill observations.
func (s *seeder) seedSkills(ctx context.Context) error {
	proj := s.projectID()
	lucas := s.userID("lucas")

	// Prompts (the Prompts page otherwise falls back to onboarding).
	// engine must be 'mustache': the dashboard SDK's Engine enum is closed and
	// a NULL/empty engine fails client-side validation, crashing every page
	// that embeds a prompt template (Prompts list, source detail via
	// tools.list). URN kind is 'prompt', not the http tools' kind.
	if err := s.q.SkillsInsertPromptTemplates(ctx, repo.SkillsInsertPromptTemplatesParams{
		TriageID:         s.fixed("0000-4000-a000-00000000dd01"),
		ProjectID:        proj,
		TriageHistoryID:  s.fixed("0000-4000-a000-00000000dd11"),
		LatencyID:        s.fixed("0000-4000-a000-00000000dd02"),
		LatencyHistoryID: s.fixed("0000-4000-a000-00000000dd12"),
	}); err != nil {
		return fmt.Errorf("insert demo prompt templates: %w", err)
	}

	// Skills: three manual skills with one version each and an open edit
	// suggestion on support-refunds (lights up the review flow). Version shas
	// are display-only on the read path, so sha256(content) stands in for the
	// server's canonicalized sha.
	for i := 1; i <= 3; i++ {
		sk := skillsManual[i-1]
		if err := s.q.SkillsInsertSkill(ctx, repo.SkillsInsertSkillParams{
			ID:          s.skillID(i),
			ProjectID:   proj,
			Name:        skillNames[i-1],
			DisplayName: sk.displayName,
			Summary:     sk.summary,
			SeenCount:   int64(14 - i*3),
		}); err != nil {
			return fmt.Errorf("insert demo skill %d: %w", i, err)
		}
		if err := s.q.SkillsInsertInitialVersion(ctx, repo.SkillsInsertInitialVersionParams{
			ID:              s.skillVersionID(i),
			SkillID:         s.skillID(i),
			CreatedByUserID: lucas,
			Content:         sk.content,
		}); err != nil {
			return fmt.Errorf("insert demo skill %d version: %w", i, err)
		}
	}

	suggestionID := s.fixed("0000-4000-a000-000000005301")
	if err := s.q.SkillsInsertEditSuggestion(ctx, repo.SkillsInsertEditSuggestionParams{
		ID:            suggestionID,
		ProjectID:     proj,
		SkillID:       s.skillID(1),
		BaseVersionID: s.skillVersionID(1),
	}); err != nil {
		return fmt.Errorf("insert demo skill edit suggestion: %w", err)
	}

	// The diff MUST carry ---/+++ file headers: gitdiff.Parse yields zero file
	// fragments for a bare hunk, skilldiff.Apply then fails and the dashboard
	// retires the suggestion as "no longer lines up with the manifest".
	if err := s.q.SkillsInsertEditSuggestionChange(ctx, repo.SkillsInsertEditSuggestionChangeParams{
		ProjectID:    proj,
		SuggestionID: suggestionID,
	}); err != nil {
		return fmt.Errorf("insert demo skill edit suggestion change: %w", err)
	}

	// Skill efficacy, activations, and drift.
	// Activations: 1-3 reconciled skill_observations per odd (Claude) chat,
	// with metrics_synced_at/efficacy_enqueued_at stamped so the metrics-sync
	// worker and the efficacy sweeper treat them as already processed (they
	// would otherwise re-emit ClickHouse mappings / enqueue real judge work).
	// Drift: runbook gets a v2 and one active plugin distribution per skill,
	// then a fresh latest-observation per machine splits the fleet into
	// on-target / drifted / indeterminate.
	runbookV2 := s.skillsRunbookV2()
	if err := s.q.SkillsInsertRunbookV2(ctx, repo.SkillsInsertRunbookV2Params{
		ID:              runbookV2,
		SkillID:         s.skillID(3),
		CreatedByUserID: lucas,
		Content:         skillContentRunbookV2,
	}); err != nil {
		return fmt.Errorf("insert demo runbook v2: %w", err)
	}

	if err := s.q.SkillsInsertVersionLineage(ctx, repo.SkillsInsertVersionLineageParams{
		SkillVersionID:       runbookV2,
		SkillID:              s.skillID(3),
		DerivedFromVersionID: s.skillVersionID(3),
	}); err != nil {
		return fmt.Errorf("insert demo runbook v2 lineage: %w", err)
	}

	if err := s.q.SkillsInsertDistributions(ctx, repo.SkillsInsertDistributionsParams{
		Id1:             s.fixed("0000-4000-a000-0000000054a1"),
		ProjectID:       proj,
		SkillId1:        s.skillID(1),
		CreatedByUserID: lucas,
		Id2:             s.fixed("0000-4000-a000-0000000054a2"),
		SkillId2:        s.skillID(2),
		Id3:             s.fixed("0000-4000-a000-0000000054a3"),
		SkillId3:        s.skillID(3),
	}); err != nil {
		return fmt.Errorf("insert demo skill distributions: %w", err)
	}

	for i := 1; i <= bulkChats; i++ {
		if i%2 == 0 {
			continue
		}
		// Same skill-per-chat formula as the ClickHouse Skill hook rows and the
		// efficacy mappings: skill_pos = 1 + (((i-1)/2) % 3).
		skillPos := 1 + (((i - 1) / 2) % 3)
		// Runbook activations within the last ~5 days (chatDayOff(i) <= 4) are
		// on v2: the activation timeline shows the version rollout, and the
		// drift split below stays consistent with it. Mirrored in the
		// ClickHouse efficacy inserts (day_off <= 4).
		versionID := s.skillVersionID(skillPos)
		if skillPos == 3 && s.chatDayOff(i) <= 4 {
			versionID = runbookV2
		}
		chatTS := s.chatTS(i)
		owner := s.chatOwner(i)
		for m := 1; m <= 1+(i%3); m++ {
			if err := s.q.SkillsInsertActivation(ctx, repo.SkillsInsertActivationParams{
				ID:             s.det(fmt.Sprintf("skillobs-%d-%d", i, m)),
				ProjectID:      proj,
				IdempotencyKey: fmt.Sprintf("demo-skillobs-%d-%d", i, m),
				UserID:         s.userID(owner.handle),
				UserEmail:      s.email(owner.handle),
				Hostname:       owner.hostname,
				SessionID:      s.chatID(i).String(),
				SkillName:      skillNames[skillPos-1],
				SkillVersionID: versionID,
				SeenAt:         skillsTS(chatTS.Add(45*time.Second + time.Duration(m-1)*2*time.Minute)),
				SkillID:        s.skillID(skillPos),
				ReconciledAt:   skillsTS(chatTS.Add(50 * time.Second)),
				SyncedAt:       skillsTS(chatTS.Add(55 * time.Second)),
			}); err != nil {
				return fmt.Errorf("insert demo skill activation %d-%d: %w", i, m, err)
			}
		}
	}

	// Drift split for runbook (which now has v2): latest observation per
	// machine decides its state — 3 on v2 (on-target), 2 on v1 (drifted),
	// 1 with no resolvable version (indeterminate).
	for i := 1; i <= 6; i++ {
		u := demoUsers[i-1]
		shaVersion := s.skillVersionID(3)
		if i <= 3 {
			shaVersion = runbookV2
		}
		var versionID uuid.NullUUID
		switch {
		case i <= 3:
			versionID = uuid.NullUUID{UUID: runbookV2, Valid: true}
		case i <= 5:
			versionID = uuid.NullUUID{UUID: s.skillVersionID(3), Valid: true}
		}
		if err := s.q.SkillsInsertDriftObservation(ctx, repo.SkillsInsertDriftObservationParams{
			ID:             s.det(fmt.Sprintf("skillobs-drift-%d", i)),
			ProjectID:      proj,
			IdempotencyKey: fmt.Sprintf("demo-skillobs-drift-%d", i),
			UserID:         s.userID(u.handle),
			UserEmail:      s.email(u.handle),
			Hostname:       u.hostname,
			ShaVersionID:   shaVersion,
			SeenAt:         skillsTS(s.now.Add(-2*time.Hour + time.Duration(i)*7*time.Minute)),
			SkillID:        s.skillID(3),
			SkillVersionID: versionID,
		}); err != nil {
			return fmt.Errorf("insert demo skill drift observation %d: %w", i, err)
		}
	}

	// Unknown activations (Skills list footer): reconciled with error codes.
	mateo, hana := demoUsers[3], demoUsers[4]
	if err := s.q.SkillsInsertUnknownActivations(ctx, repo.SkillsInsertUnknownActivationsParams{
		Id1:        s.det("skillobs-unknown-1"),
		ProjectID:  proj,
		UserId1:    s.userID(mateo.handle),
		UserEmail1: s.email(mateo.handle),
		Hostname1:  mateo.hostname,
		Id2:        s.det("skillobs-unknown-2"),
		UserId2:    s.userID(hana.handle),
		UserEmail2: s.email(hana.handle),
		Hostname2:  hana.hostname,
	}); err != nil {
		return fmt.Errorf("insert demo unknown skill activations: %w", err)
	}

	return nil
}
