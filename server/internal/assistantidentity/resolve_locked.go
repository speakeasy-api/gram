package assistantidentity

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	workloadrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
)

// ResolveLocked resolves identity on the caller's transaction and keeps its
// binding, issuer, admission and assignment stable until that transaction ends.
// It follows the writer lock order: assistant, trigger, then workload issuer.
// Call before taking any of those locks in another order. Unlike Resolve it
// neither acquires another connection nor opens a separate read snapshot.
func (s *Service) ResolveLocked(ctx context.Context, tx pgx.Tx, org string, project, assistant, trigger uuid.UUID) (Resolution, error) {
	if org == "" || project == uuid.Nil || assistant == uuid.Nil || trigger == uuid.Nil {
		return Resolution{}, ErrInvalidIdentity
	}
	q := repo.New(tx)
	a, err := q.LockAssistant(ctx, repo.LockAssistantParams{ProjectID: project, AssistantID: assistant})
	if err != nil {
		return Resolution{}, resourceError("lock identity assistant", err)
	}
	if a.OrganizationID != org {
		return Resolution{}, ErrInvalidIdentity
	}
	roots, err := q.LockTriggers(ctx, repo.LockTriggersParams{ProjectID: project, TriggerIds: []uuid.UUID{trigger}})
	if err != nil {
		return Resolution{}, fmt.Errorf("lock identity trigger: %w", err)
	}
	if len(roots) != 1 {
		return Resolution{}, ErrInvalidIdentity
	}
	binding, err := q.GetAssistantTriggerBinding(ctx, repo.GetAssistantTriggerBindingParams{ProjectID: project, AssistantID: assistant, TriggerID: trigger})
	if err != nil {
		return Resolution{}, resourceError("read locked identity binding", err)
	}
	if binding.OrganizationID != org {
		return Resolution{}, ErrInvalidIdentity
	}
	// Every admission/assignment writer serializes on the issuer, including
	// insertion of previously absent rows. Locking just existing child rows would
	// not protect against an admission or assignment appearing concurrently.
	if _, err := workloadrepo.New(tx).LockWorkloadIssuerForWrite(ctx, workloadrepo.LockWorkloadIssuerForWriteParams{OrganizationID: org, ID: binding.WorkloadIssuerID}); err != nil {
		return Resolution{}, resourceError("lock identity issuer", err)
	}
	return resolve(ctx, tx, org, project, assistant, trigger)
}
