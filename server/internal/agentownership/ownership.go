package agentownership

import (
	"context"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/agents/lifecycle"
	"github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// OwnerReassignmentReason is the stable reason an agent can no longer rely on
// its current human owner. The latch is cleared only by explicit reassignment.
type OwnerReassignmentReason string

const (
	OwnerReassignmentReasonOwnerDeleted   OwnerReassignmentReason = "owner_deleted"
	OwnerReassignmentReasonOwnerInactive  OwnerReassignmentReason = "owner_inactive"
	OwnerReassignmentReasonMembershipLost OwnerReassignmentReason = "organization_membership_lost"
)

// SystemActor attributes owner loss observed from an external identity event.
var SystemActor = urn.Principal{Type: urn.PrincipalTypeUser, ID: "system"}

// LatchOwnerLossByUser blocks every current ownership of a deleted user.
func LatchOwnerLossByUser(ctx context.Context, dbtx repo.DBTX, ownerUserID string, reason OwnerReassignmentReason, actor urn.Principal, actorDisplayName *string) error {
	if !reason.valid() {
		return fmt.Errorf("invalid owner reassignment reason %q", reason)
	}
	rows, err := repo.New(dbtx).LatchAgentsForOwnerLossByUser(ctx, repo.LatchAgentsForOwnerLossByUserParams{
		OwnerUserID:             ownerUserID,
		OwnerReassignmentReason: conv.ToPGText(string(reason)),
	})
	if err != nil {
		return fmt.Errorf("latch agents for owner loss: %w", err)
	}
	return logOwnerLoss(ctx, dbtx, rows, actor, actorDisplayName)
}

// LatchOwnerLossByMembership blocks current ownership in one organization when
// that owner loses or deactivates their membership.
func LatchOwnerLossByMembership(ctx context.Context, dbtx repo.DBTX, organizationID, ownerUserID string, reason OwnerReassignmentReason, actor urn.Principal, actorDisplayName *string) error {
	if !reason.valid() {
		return fmt.Errorf("invalid owner reassignment reason %q", reason)
	}
	if ownerUserID == "" {
		return nil
	}
	rows, err := repo.New(dbtx).LatchAgentsForOwnerLossByMembership(ctx, repo.LatchAgentsForOwnerLossByMembershipParams{
		OrganizationID:          organizationID,
		OwnerUserID:             ownerUserID,
		OwnerReassignmentReason: conv.ToPGText(string(reason)),
	})
	if err != nil {
		return fmt.Errorf("latch agents for membership loss: %w", err)
	}
	return logOwnerLoss(ctx, dbtx, rows, actor, actorDisplayName)
}

func logOwnerLoss(ctx context.Context, dbtx repo.DBTX, rows []repo.Agent, actor urn.Principal, actorDisplayName *string) error {
	logger := audit.NewLogger()
	for _, after := range rows {
		before := agentAuditSnapshot(after)
		before.OwnerReassignmentRequiredAt = nil
		before.OwnerReassignmentReason = nil
		if err := logger.LogAgent(ctx, dbtx, audit.LogAgentEvent{
			OrganizationID:   after.OrganizationID,
			AgentURN:         urn.NewAgentIdentity(after.ID.String()),
			Actor:            actor,
			ActorDisplayName: actorDisplayName,
			Action:           audit.ActionAgentOwnerLoss,
			Name:             after.Name,
			Before:           before,
			After:            agentAuditSnapshot(after),
		}); err != nil {
			return fmt.Errorf("log owner-loss audit event: %w", err)
		}
	}
	return nil
}

// AgentAuditSnapshot returns the bounded audit view shared by lifecycle and
// ownership mutations.
func AgentAuditSnapshot(agent repo.Agent) *audit.AgentSnapshot {
	return agentAuditSnapshot(agent)
}

func agentAuditSnapshot(agent repo.Agent) *audit.AgentSnapshot {
	snapshot := &audit.AgentSnapshot{
		OwnerUserID:                 agent.OwnerUserID,
		OwnerReassignmentRequiredAt: nil,
		OwnerReassignmentReason:     nil,
		Name:                        agent.Name,
		Lifecycle:                   string(lifecycle.Derive(agent)),
	}
	if agent.OwnerReassignmentRequiredAt.Valid {
		value := agent.OwnerReassignmentRequiredAt.Time.Format(time.RFC3339Nano)
		snapshot.OwnerReassignmentRequiredAt = &value
	}
	if agent.OwnerReassignmentReason.Valid {
		value := agent.OwnerReassignmentReason.String
		snapshot.OwnerReassignmentReason = &value
	}
	return snapshot
}

func (r OwnerReassignmentReason) valid() bool {
	switch r {
	case OwnerReassignmentReasonOwnerDeleted, OwnerReassignmentReasonOwnerInactive, OwnerReassignmentReasonMembershipLost:
		return true
	default:
		return false
	}
}
