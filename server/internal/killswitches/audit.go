package killswitches

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// NewAuditBeforeCommitHook records the typed lifecycle audit entry and its
// cataloged outbox event inside the lifecycle transaction, so a state change,
// its completed operation receipt, its audit row, and its outbox row commit or
// roll back together.
func NewAuditBeforeCommitHook(auditLogger *audit.Logger) BeforeCommitHook {
	return func(ctx context.Context, queries LifecycleTransactionQueries, event MutationEvent) error {
		action, err := lifecycleAuditAction(event.Operation)
		if err != nil {
			return err
		}
		prescriptionID, err := parsePrescriptionID(event.Result.PrescriptionID)
		if err != nil {
			return err
		}
		if err := auditLogger.LogKillswitchLifecycle(ctx, queries, audit.LogKillswitchLifecycleEvent{
			OrganizationID:   string(event.OrganizationID),
			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, event.ActorUserID),
			Action:           action,
			PrescriptionURN:  urn.NewKillswitchPrescription(prescriptionID),
			Version:          event.Result.Version,
			State:            string(event.Result.State),
			Operation:        string(event.Operation),
			OperationReceipt: event.OperationID,
		}); err != nil {
			return fmt.Errorf("audit killswitch lifecycle transition: %w", err)
		}
		return nil
	}
}

func lifecycleAuditAction(operation MutationOperation) (audit.Action, error) {
	switch operation {
	case MutationOperationActivate, MutationOperationReactivate:
		return audit.ActionKillswitchActivate, nil
	case MutationOperationChange:
		return audit.ActionKillswitchChange, nil
	case MutationOperationDeactivate:
		return audit.ActionKillswitchDeactivate, nil
	default:
		return "", fmt.Errorf("no audit action for killswitch operation %q", operation)
	}
}
