//nolint:exhaustruct // Audit and repository literals omit optional fields intentionally.
package toolsets

import (
	"context"
	"errors"
	"log/slog"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ToolExposureChange is the incremental edit a caller asks for. Exactly one of
// Add or Remove is populated. It deliberately carries a delta rather than a
// replacement list: the whole-array write used by UpdateToolset can only be
// safe when the reader and the writer are the same transaction, and a caller
// outside that transaction cannot promise that.
type ToolExposureChange struct {
	Add    []urn.Tool
	Remove []urn.Tool
}

// ToolExposureResult reports what the append actually did, separately from what
// was asked for, so a caller can tell a real change from a no-op.
type ToolExposureResult struct {
	ToolsetID      uuid.UUID
	ToolsetSlug    string
	ToolsetName    string
	VersionBefore  int64
	VersionAfter   int64
	ToolURNsBefore []urn.Tool
	ToolURNsAfter  []urn.Tool
	// Applied names the URNs this call added or removed. Unchanged names the
	// URNs that were already in the requested state, which is a no-op rather
	// than a failure.
	Applied   []urn.Tool
	Unchanged []urn.Tool
	Changed   bool
}

// ChangeToolsetToolsInTransaction adds or removes named tools on one toolset
// inside a caller-owned transaction.
//
// The read and the write happen under the same row lock in the same
// transaction, so the new version is always computed from the committed list
// rather than from whatever the caller last saw. expectedVersion is the
// caller's separate claim about which list it was reasoning over: a mismatch
// refuses instead of appending to a list the caller never read. The caller must
// authorize the target first.
func ChangeToolsetToolsInTransaction(ctx context.Context, tx pgx.Tx, logger *slog.Logger, auditLogger *audit.Logger, actor *contextvalues.AuthContext, toolsetID uuid.UUID, expectedVersion int64, change ToolExposureChange) (ToolExposureResult, error) {
	if actor == nil || actor.ProjectID == nil || auditLogger == nil || logger == nil {
		return ToolExposureResult{}, oops.E(oops.CodeUnauthorized, nil, "missing toolset tool exposure actor")
	}
	if len(change.Add) == 0 && len(change.Remove) == 0 {
		return ToolExposureResult{}, oops.E(oops.CodeBadRequest, nil, "no tools requested")
	}
	if len(change.Add) > 0 && len(change.Remove) > 0 {
		return ToolExposureResult{}, oops.E(oops.CodeBadRequest, nil, "a single call adds or removes tools, never both")
	}

	toolsetRepo := repo.New(tx)
	candidate, err := toolsetRepo.GetToolsetByIDAndProject(ctx, repo.GetToolsetByIDAndProjectParams{ID: toolsetID, ProjectID: *actor.ProjectID})
	if err != nil {
		return ToolExposureResult{}, oops.E(oops.CodeNotFound, err, "toolset not found")
	}
	if candidate.OrganizationID != actor.ActiveOrganizationID {
		return ToolExposureResult{}, oops.E(oops.CodeNotFound, nil, "toolset not found")
	}
	locked, err := toolsetRepo.GetToolsetForUpdate(ctx, repo.GetToolsetForUpdateParams{Slug: candidate.Slug, ProjectID: *actor.ProjectID})
	if err != nil || locked.ID != toolsetID || locked.OrganizationID != actor.ActiveOrganizationID {
		return ToolExposureResult{}, oops.E(oops.CodeConflict, err, "toolset changed concurrently")
	}

	latest, err := toolsetRepo.GetLatestToolsetVersion(ctx, toolsetID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		latest = repo.ToolsetVersion{}
	case err != nil:
		return ToolExposureResult{}, oops.E(oops.CodeUnexpected, err, "failed to read the toolset's current tools")
	}
	if latest.Version != expectedVersion {
		return ToolExposureResult{}, oops.E(oops.CodeConflict, nil, "the toolset's tools changed since they were read")
	}

	before := slices.Clone(latest.ToolUrns)
	after, applied, unchanged := applyToolExposureChange(before, change)
	result := ToolExposureResult{
		ToolsetID: locked.ID, ToolsetSlug: locked.Slug, ToolsetName: locked.Name,
		VersionBefore: latest.Version, VersionAfter: latest.Version,
		ToolURNsBefore: before, ToolURNsAfter: before,
		Applied: applied, Unchanged: unchanged, Changed: false,
	}
	if len(applied) == 0 {
		return result, nil
	}

	beforeView, err := mv.DescribeToolset(ctx, logger, tx, mv.ProjectID(*actor.ProjectID), mv.ToolsetSlug(locked.Slug), nil, nil)
	if err != nil {
		return ToolExposureResult{}, oops.E(oops.CodeUnexpected, err, "failed to describe the toolset before changing its tools")
	}
	// Both columns are NOT NULL, and a toolset with no version yet reads back
	// as a nil slice rather than an empty one.
	resources := latest.ResourceUrns
	if resources == nil {
		resources = []urn.Resource{}
	}
	if after == nil {
		after = []urn.Tool{}
	}
	if _, err := toolsetRepo.CreateToolsetVersion(ctx, repo.CreateToolsetVersionParams{
		ToolsetID:     toolsetID,
		Version:       latest.Version + 1,
		ToolUrns:      after,
		ResourceUrns:  resources,
		PredecessorID: uuid.NullUUID{UUID: latest.ID, Valid: latest.ID != uuid.Nil},
	}); err != nil {
		return ToolExposureResult{}, oops.E(oops.CodeUnexpected, err, "failed to record the toolset's new tools")
	}
	afterView, err := mv.DescribeToolset(ctx, logger, tx, mv.ProjectID(*actor.ProjectID), mv.ToolsetSlug(locked.Slug), nil, nil)
	if err != nil {
		return ToolExposureResult{}, oops.E(oops.CodeUnexpected, err, "failed to describe the toolset after changing its tools")
	}
	if err := auditLogger.LogToolsetUpdate(ctx, tx, audit.LogToolsetUpdateEvent{
		OrganizationID:        locked.OrganizationID,
		ProjectID:             *actor.ProjectID,
		Actor:                 urn.NewPrincipal(urn.PrincipalTypeUser, actor.UserID),
		ActorDisplayName:      actor.Email,
		ActorSlug:             nil,
		ToolsetURN:            urn.NewToolset(locked.ID),
		ToolsetName:           locked.Name,
		ToolsetSlug:           locked.Slug,
		ToolsetVersionAfter:   afterView.ToolsetVersion,
		ToolsetSnapshotBefore: beforeView,
		ToolsetSnapshotAfter:  afterView,
	}); err != nil {
		return ToolExposureResult{}, oops.E(oops.CodeUnexpected, err, "failed to record the toolset tool change")
	}

	result.VersionAfter = latest.Version + 1
	result.ToolURNsAfter = after
	result.Changed = true
	return result, nil
}

// applyToolExposureChange keeps the surviving order stable so a version diff
// reads as the edit that was made rather than as a reshuffle.
func applyToolExposureChange(current []urn.Tool, change ToolExposureChange) (after, applied, unchanged []urn.Tool) {
	present := make(map[string]bool, len(current))
	for _, tool := range current {
		present[tool.String()] = true
	}
	after = slices.Clone(current)
	applied, unchanged = []urn.Tool{}, []urn.Tool{}
	if len(change.Remove) > 0 {
		removing := make(map[string]bool, len(change.Remove))
		for _, tool := range change.Remove {
			if present[tool.String()] {
				removing[tool.String()] = true
				applied = append(applied, tool)
				continue
			}
			unchanged = append(unchanged, tool)
		}
		after = after[:0]
		for _, tool := range current {
			if !removing[tool.String()] {
				after = append(after, tool)
			}
		}
		return after, applied, unchanged
	}
	for _, tool := range change.Add {
		if present[tool.String()] {
			unchanged = append(unchanged, tool)
			continue
		}
		present[tool.String()] = true
		after = append(after, tool)
		applied = append(applied, tool)
	}
	return after, applied, unchanged
}
