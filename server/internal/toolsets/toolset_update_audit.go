package toolsets

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// toolsetUpdateAudit is one toolset change to record as a toolset:update entry.
type toolsetUpdateAudit struct {
	// ToolsetID is the audited toolset.
	ToolsetID uuid.UUID

	// Name is the toolset's display name after the change.
	Name string

	// Slug is the toolset's slug after the change.
	Slug string

	// Before is the toolset view read before the change was written.
	Before *types.Toolset

	// After is the toolset view read, in the same transaction, after the
	// change was written. Its version is the one the entry reports.
	After *types.Toolset
}

// toolsetUpdateEvent builds the toolset:update entry for change, attributed to
// the calling user. Callers log it in the transaction that wrote the change so
// the entry commits or rolls back with it.
func toolsetUpdateEvent(actor *contextvalues.AuthContext, change toolsetUpdateAudit) audit.LogToolsetUpdateEvent {
	return audit.LogToolsetUpdateEvent{
		OrganizationID:        actor.ActiveOrganizationID,
		ProjectID:             *actor.ProjectID,
		Actor:                 urn.NewPrincipal(urn.PrincipalTypeUser, actor.UserID),
		ActorDisplayName:      actor.Email,
		ActorSlug:             nil,
		ToolsetURN:            urn.NewToolset(change.ToolsetID),
		ToolsetName:           change.Name,
		ToolsetSlug:           change.Slug,
		ToolsetVersionAfter:   change.After.ToolsetVersion,
		ToolsetSnapshotBefore: change.Before,
		ToolsetSnapshotAfter:  change.After,
	}
}

// describeAndLogToolsetUpdate finishes a describe-before, mutate,
// describe-after sequence for a handler that changed one toolset field in tx:
// it reads the toolset back through tx, records the toolset:update entry
// against before, and returns the view read after the change.
func (s *Service) describeAndLogToolsetUpdate(ctx context.Context, tx pgx.Tx, authCtx *contextvalues.AuthContext, slug types.Slug, before *types.Toolset) (*types.Toolset, error) {
	after, err := mv.DescribeToolset(ctx, s.logger, tx, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(slug), new(s.toolsetCache.SkipCache()), nil)
	if err != nil {
		return nil, err
	}

	toolsetID, err := uuid.Parse(after.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "invalid toolset id").LogError(ctx, s.logger)
	}

	if err := s.audit.LogToolsetUpdate(ctx, tx, toolsetUpdateEvent(authCtx, toolsetUpdateAudit{
		ToolsetID: toolsetID,
		Name:      after.Name,
		Slug:      string(after.Slug),
		Before:    before,
		After:     after,
	})); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log toolset update").LogError(ctx, s.logger)
	}
	return after, nil
}
