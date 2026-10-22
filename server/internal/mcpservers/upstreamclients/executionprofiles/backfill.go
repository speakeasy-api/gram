package executionprofiles

import (
	"context"
	"fmt"
)

// Report summarizes the profiles of one organization. It carries counts only.
type Report struct {
	// Mode is "apply" or "dry_run".
	Mode string `json:"mode"`

	// Targets counts assistant and root trigger pairs, each with a profile per
	// execution context.
	Targets int `json:"targets"`

	// VersionsWritten counts profile versions this run recorded, or a dry run
	// would record.
	VersionsWritten int `json:"versions_written"`

	// Gaps counts, per execution context, the profile and server pairs the
	// profiles' latest versions hold no choice for, by reason.
	Gaps map[Context]map[GapReason]int `json:"gaps"`
}

// Backfill records a profile for every live assistant and root trigger pair in
// the organization, in each execution context, deciding each one's choices
// from the configuration that serves its turns today. With apply false it
// decides without writing or locking any profile. With apply true each
// profile is bound in its own transaction, so a turn binding it waits only
// for that profile. It is idempotent: a rerun only adds choices for servers
// that had none and can now be decided.
func Backfill(ctx context.Context, db TxStarter, organizationID string, apply bool) (Report, error) {
	report := Report{Mode: "dry_run", Targets: 0, VersionsWritten: 0, Gaps: map[Context]map[GapReason]int{}}
	if apply {
		report.Mode = "apply"
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return report, fmt.Errorf("begin execution profile backfill: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	q := New(tx)
	targets, err := q.ListProfileTargets(ctx, organizationID)
	if err != nil {
		return report, fmt.Errorf("list execution profile targets: %w", err)
	}
	for _, t := range targets {
		report.Targets++
		for _, execCtx := range Contexts {
			target := Target{
				OrganizationID: organizationID,
				ProjectID:      t.ProjectID,
				AssistantID:    t.AssistantID.UUID,
				TriggerID:      t.TriggerID.UUID,
				Context:        execCtx,
			}
			var gaps []Gap
			if apply {
				before, err := preview(ctx, q, target)
				if err != nil {
					return report, err
				}
				binding, err := Bind(ctx, db, target, SourceBackfill)
				if err != nil {
					return report, err
				}
				if binding.Version != before.version {
					report.VersionsWritten++
				}
				gaps = binding.Gaps
			} else {
				p, err := preview(ctx, q, target)
				if err != nil {
					return report, err
				}
				if p.writes() {
					report.VersionsWritten++
				}
				gaps = p.gaps
			}
			for _, gap := range gaps {
				if report.Gaps[execCtx] == nil {
					report.Gaps[execCtx] = map[GapReason]int{}
				}
				report.Gaps[execCtx][gap.Reason]++
			}
		}
	}
	return report, nil
}
