package toolsets

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// SetHostedNetworkAccessInTransaction changes only the canonical hosted policy
// within a caller-owned transaction. The caller must authorize the target and
// hold the organization's ingress and project admission locks first.
func SetHostedNetworkAccessInTransaction(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, actor *contextvalues.AuthContext, toolsetID uuid.UUID, mode networkaccess.Mode) error {
	if actor == nil || actor.ProjectID == nil || auditLogger == nil {
		return oops.E(oops.CodeUnauthorized, nil, "missing hosted MCP actor")
	}
	toolsets := repo.New(tx)
	candidate, err := toolsets.GetToolsetByIDAndProject(ctx, repo.GetToolsetByIDAndProjectParams{ID: toolsetID, ProjectID: *actor.ProjectID})
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "hosted toolset not found")
	}
	if candidate.OrganizationID != actor.ActiveOrganizationID {
		return oops.E(oops.CodeNotFound, nil, "hosted toolset not found")
	}
	locked, err := toolsets.GetToolsetForUpdate(ctx, repo.GetToolsetForUpdateParams{Slug: candidate.Slug, ProjectID: *actor.ProjectID})
	if err != nil || locked.ID != toolsetID || locked.OrganizationID != actor.ActiveOrganizationID {
		return oops.E(oops.CodeConflict, err, "hosted toolset changed concurrently")
	}
	// A mode change leaves visibility and address alone, so no root is cleared.
	_, err = hostedmcp.Sync(ctx, tx, auditLogger, hostedActor(actor), locked, &mode)
	return err //nolint:wrapcheck // hostedmcp returns oops errors whose codes reach the caller.
}

// syncHostedServer must run under the toolset row lock.
func (s *Service) syncHostedServer(ctx context.Context, tx pgx.Tx, actor *contextvalues.AuthContext, toolset repo.Toolset, requested *networkaccess.Mode) ([]uuid.UUID, error) {
	return hostedmcp.Sync(ctx, tx, s.audit, hostedActor(actor), toolset, requested) //nolint:wrapcheck // oops errors pass through.
}

func (s *Service) deleteHostedServer(ctx context.Context, tx pgx.Tx, actor *contextvalues.AuthContext, toolset repo.Toolset) ([]uuid.UUID, error) {
	return hostedmcp.Delete(ctx, tx, s.audit, hostedActor(actor), toolset) //nolint:wrapcheck // oops errors pass through.
}

// reconcileCustomDomains runs after commit for domains whose root was cleared.
func (s *Service) reconcileCustomDomains(ctx context.Context, customDomainIDs []uuid.UUID) error {
	if s.temporalEnv == nil {
		return nil
	}
	var errs []error
	for _, id := range customDomainIDs {
		if _, err := (&background.CustomDomainRegistrationClient{TemporalEnv: s.temporalEnv}).ExecuteCustomDomainReconcile(ctx, id); err != nil {
			errs = append(errs, oops.E(oops.CodeUnexpected, err, "start custom domain reconciliation").LogError(ctx, s.logger))
		}
	}
	return errors.Join(errs...)
}

func hostedActor(actor *contextvalues.AuthContext) hostedmcp.Actor {
	if actor == nil {
		return hostedmcp.Actor{UserID: "", Email: nil, System: ""}
	}
	return hostedmcp.Actor{UserID: actor.UserID, Email: actor.Email, System: ""}
}
