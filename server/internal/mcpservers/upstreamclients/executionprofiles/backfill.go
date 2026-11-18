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

	// FirstVersions counts the versions in VersionsWritten that are a
	// profile's first: no turn has run under that assistant, trigger and
	// execution context since profiles were recorded. A turn records its own
	// profile when its credential is issued.
	FirstVersions int `json:"first_versions"`

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
	report := Report{Mode: "dry_run", Targets: 0, VersionsWritten: 0, FirstVersions: 0, Gaps: make(map[Context]map[GapReason]int)}
	if apply {
		report.Mode = "apply"
	}
	targets, err := listTargets(ctx, db, organizationID)
	if err != nil {
		return report, err
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
			var written, first bool
			var gaps []Gap
			if apply {
				binding, err := Bind(ctx, db, target, SourceBackfill)
				if err != nil {
					return report, err
				}
				written, first, gaps = binding.Written, binding.Version == 1, binding.Gaps
			} else {
				p, err := previewTarget(ctx, db, target)
				if err != nil {
					return report, err
				}
				written, first, gaps = p.writes(), p.version == 0, p.gaps
			}
			if written {
				report.VersionsWritten++
				if first {
					report.FirstVersions++
				}
			}
			for _, gap := range gaps {
				if report.Gaps[execCtx] == nil {
					report.Gaps[execCtx] = make(map[GapReason]int)
				}
				report.Gaps[execCtx][gap.Reason]++
			}
		}
	}
	return report, nil
}

// listTargets reads the organization's targets in a transaction it closes
// before any target is bound, so a run never holds two connections.
func listTargets(ctx context.Context, db TxStarter, organizationID string) ([]ListProfileTargetsRow, error) {
	tx, err := db.BeginTx(ctx, readCommitted)
	if err != nil {
		return nil, fmt.Errorf("begin execution profile target list: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	targets, err := New(tx).ListProfileTargets(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list execution profile targets: %w", err)
	}
	return targets, nil
}

// previewTarget decides what a bind under target would record, in a
// transaction of its own that writes nothing.
func previewTarget(ctx context.Context, db TxStarter, target Target) (plan, error) {
	tx, err := db.BeginTx(ctx, readCommitted)
	if err != nil {
		return plan{}, fmt.Errorf("begin execution profile preview: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	return preview(ctx, New(tx), target)
}
