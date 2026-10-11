package admission

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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

// ActorFromContext reads the acting user from the request's auth context. A
// background job or a connection-less surface has none, and is never an
// administrator here: its refusals stay refusals.
func ActorFromContext(ctx context.Context) Actor {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.UserID == "" {
		return Actor{UserID: "", Email: nil, OrgAdmin: false}
	}
	return Actor{UserID: authCtx.UserID, Email: authCtx.Email, OrgAdmin: authCtx.IsAdmin}
}

// WithApprover lets the guard convert an administrator's refused write into a
// recorded approval. Without one, every uncovered audience is refused.
func (g *Guard) WithApprover(approver Approver) *Guard {
	if g != nil {
		g.approver = approver
	}
	return g
}
