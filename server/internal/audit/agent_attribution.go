package audit

import (
	"context"
	"errors"

	"github.com/google/uuid"

	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var ErrInvalidAgentManagementAttribution = errors.New("invalid agent management attribution")

// AgentManagementTargetResolver loads an affected agent by authenticated tenant
// and ID. A transaction-bound agents repository satisfies this interface.
type AgentManagementTargetResolver interface {
	GetAgentByID(context.Context, agentrepo.GetAgentByIDParams) (agentrepo.Agent, error)
}

// AgentManagementOwnerChange carries ownership identities when a transition
// has one. Empty values mean the transition has no corresponding owner.
type AgentManagementOwnerChange struct {
	FormerOwnerUserID      string
	ReplacementOwnerUserID string
}

// AgentManagementAttribution is the shared, identifier-only envelope consumed
// by agent-management audit events. Its actor is derived exclusively from
// trusted request identity; owners can never stand in for it.
type AgentManagementAttribution struct {
	OrganizationID         string
	Actor                  urn.Principal
	AffectedAgentID        uuid.UUID
	CurrentOwnerUserID     string
	FormerOwnerUserID      string
	ReplacementOwnerUserID string
}

// NewAgentManagementAttribution validates tenant binding and builds an audit
// envelope without inferring actor identity from owners or credential IDs. All
// validation failures intentionally return the same error and disclose no IDs.
func NewAgentManagementAttribution(
	ctx context.Context,
	agents AgentManagementTargetResolver,
	agentID uuid.UUID,
	ownerChange AgentManagementOwnerChange,
) (AgentManagementAttribution, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" || agents == nil || agentID == uuid.Nil {
		return AgentManagementAttribution{}, ErrInvalidAgentManagementAttribution
	}

	actor, ok := contextvalues.AuthenticatedActor(ctx)
	if !ok {
		return AgentManagementAttribution{}, ErrInvalidAgentManagementAttribution
	}

	// The lookup is pinned to the authenticated tenant. Missing and cross-tenant
	// agents intentionally collapse to the same attribution error.
	target, err := agents.GetAgentByID(ctx, agentrepo.GetAgentByIDParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ID:             agentID,
	})
	if err != nil || target.ID != agentID || target.OrganizationID != authCtx.ActiveOrganizationID ||
		target.OwnerUserID == "" {
		return AgentManagementAttribution{}, ErrInvalidAgentManagementAttribution
	}

	attribution := AgentManagementAttribution{
		OrganizationID:         authCtx.ActiveOrganizationID,
		Actor:                  actor,
		AffectedAgentID:        target.ID,
		CurrentOwnerUserID:     target.OwnerUserID,
		FormerOwnerUserID:      ownerChange.FormerOwnerUserID,
		ReplacementOwnerUserID: ownerChange.ReplacementOwnerUserID,
	}

	return attribution, nil
}
