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

// SetHostedNetworkAccessInTransaction changes only the network mode of a toolset's canonical wrapper
// within a caller-owned transaction. The caller must authorize the target and
// hold the organization's ingress and project admission locks first, and reconcile
// the returned custom domains, whose root was cleared, after commit.
func SetHostedNetworkAccessInTransaction(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, actor *contextvalues.AuthContext, toolsetID uuid.UUID, mode networkaccess.Mode) ([]uuid.UUID, error) {
	if actor == nil || actor.ProjectID == nil || auditLogger == nil {
		return nil, oops.E(oops.CodeUnauthorized, nil, "missing hosted MCP actor")
	}
	locked, err := lockHostedToolset(ctx, tx, actor, toolsetID)
	if err != nil {
		return nil, err
	}
	written, err := applyHostedServerChange(ctx, tx, auditLogger, hostedActor(actor), locked, hostedServerChange{
		Name: nil, Visibility: nil, UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, ToolVariationsGroupID: nil, NetworkAccessMode: &mode,
	})
	if err != nil {
		return nil, err
	}
	return written.ClearedRootDomainIDs, nil
}

func (s *Service) deleteHostedServer(ctx context.Context, tx pgx.Tx, actor *contextvalues.AuthContext, toolset repo.Toolset) (hostedmcp.DeleteResult, error) {
	return hostedmcp.Delete(ctx, tx, s.tracerProvider, s.audit, hostedActor(actor), toolset) //nolint:wrapcheck // oops errors pass through.
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
