package admission

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// ApproveDirectRemoteInput is one administrator's exposure write expressed as
// the standing approval it stands for: this URL, for this complete audience,
// decided by this actor. PrincipalURNs is the audience the write would reach;
// the approver unions it with any audience already approved so an earlier
// decision is widened, never narrowed.
type ApproveDirectRemoteInput struct {
	OrganizationID string
	ProjectID      uuid.UUID
	CanonicalURL   string
	PrincipalURNs  []string
	ActorUserID    string
	ActorEmail     *string
	Rationale      string
}

// Approver records a standing Shadow MCP approval inside the caller's
// transaction, so the write that needed it and the approval that permits it
// commit together or not at all. The MCP approval service implements it; the
// guard only knows the interface because that service already depends on this
// package.
type Approver interface {
	ApproveDirectRemote(ctx context.Context, tx pgx.Tx, input ApproveDirectRemoteInput) error
}

// Actor is who is making an exposure write, as far as admission cares: an
// organization administrator's own write stands as the review a block policy
// would otherwise demand, because the policy exists to catch servers reaching
// people without an administrator's say-so, and this is that say-so.
type Actor struct {
	UserID   string
	Email    *string
	OrgAdmin bool
}

type administratorKey struct{}

// WithAdministrator marks ctx as carrying an organization administrator's own
// exposure write. Only a write path sets it, and only after proving the acting
// user holds org:admin for the organization being written; see
// AdministratorWriteContext. Reads and background work never set it, so the
// guard can never turn one of their checks into a recorded approval.
func WithAdministrator(ctx context.Context, userID string, email *string) context.Context {
	if userID == "" {
		return ctx
	}
	return context.WithValue(ctx, administratorKey{}, Actor{UserID: userID, Email: email, OrgAdmin: true})
}

// ActorFromContext reads the administrator a write path recorded with
// WithAdministrator. Anything else, including a signed-in non-administrator,
// a background job and every read, is not an administrator here and its
// refusals stay refusals.
func ActorFromContext(ctx context.Context) Actor {
	if actor, ok := ctx.Value(administratorKey{}).(Actor); ok && actor.UserID != "" {
		return actor
	}
	return Actor{UserID: "", Email: nil, OrgAdmin: false}
}

// AdministratorWriteContext asks the authorization engine whether the acting
// user holds org:admin for organizationID and, when they do, marks ctx with
// WithAdministrator. A request without an acting user, or whose user acts for
// another organization, is returned unchanged. An evaluation failure is
// returned so the caller can fail its write closed rather than guess.
//
// Call this from write paths only. It is deliberately not part of the guard:
// the guard cannot tell a read from a write, and a read must never record an
// approval.
func AdministratorWriteContext(ctx context.Context, engine *authz.Engine, organizationID string) (context.Context, error) {
	if engine == nil || organizationID == "" {
		return ctx, nil
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.UserID == "" || authCtx.ActiveOrganizationID != organizationID {
		return ctx, nil
	}
	admin, err := engine.Evaluate(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: organizationID, Dimensions: nil})
	if err != nil {
		return ctx, fmt.Errorf("evaluate organization administrator for shadow MCP admission: %w", err)
	}
	if !admin {
		return ctx, nil
	}
	return WithAdministrator(ctx, authCtx.UserID, authCtx.Email), nil
}

// WithApprover lets the guard convert an administrator's refused write into a
// recorded approval. Without one, every uncovered audience is refused.
func (g *Guard) WithApprover(approver Approver) *Guard {
	if g != nil {
		g.approver = approver
	}
	return g
}
