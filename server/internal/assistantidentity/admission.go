package assistantidentity

import (
	"context"
	"fmt"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

// AdmitModel validates the live workload and selected human. Running an assistant
// requires no new capability grant. Business operations separately intersect the
// saved ceiling with current authority at their resource-specific boundaries.
func (s *Service) AdmitModel(ctx context.Context, db DB, e Execution) error {
	if err := s.CheckRollout(e); err != nil {
		return err
	}
	if err := e.Check(); err != nil {
		return fmt.Errorf("assistant execution admission: %w", err)
	}
	if s == nil || e.Issuer != s.issuer {
		return ErrInvalidIdentity
	}
	tx, err := readSnapshot(ctx, db)
	if err != nil {
		return fmt.Errorf("assistant execution admission: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	resolved, err := s.resolve(ctx, tx, e.Identity.OrganizationID, e.Identity.ProjectID, e.Identity.AssistantID, e.Identity.TriggerID)
	if err != nil {
		return fmt.Errorf("assistant execution admission: %w", err)
	}
	if err := matchExpected(resolved, e.Identity); err != nil {
		return fmt.Errorf("assistant execution admission: %w", err)
	}
	assistant, err := repo.New(tx).GetAssistant(ctx, repo.GetAssistantParams{OrganizationID: e.Identity.OrganizationID, ProjectID: e.Identity.ProjectID, AssistantID: e.Identity.AssistantID})
	if err != nil {
		return fmt.Errorf("load model assistant lifecycle: %w", err)
	}
	if assistant.Deleted || !assistant.ProjectLive || assistant.Status != "active" {
		return ErrInvalidIdentity
	}
	if e.Mode == ExecutionWorkloadHuman {
		if err := ValidateSlackDelegation(ctx, tx, e); err != nil {
			return err
		}
		active, err := orgrepo.New(tx).HasActiveOrganizationUser(ctx, orgrepo.HasActiveOrganizationUserParams{OrganizationID: e.Identity.OrganizationID, UserID: e.HumanUserID})
		if err != nil {
			return fmt.Errorf("load human membership: %w", err)
		}
		if !active {
			return ErrInvalidIdentity
		}
		principals, err := authz.ResolveUserPrincipals(ctx, tx, e.Identity.OrganizationID, e.HumanUserID)
		if err != nil {
			return fmt.Errorf("assistant execution admission: %w", err)
		}
		grants, err := authz.LoadGrants(ctx, tx, e.Identity.OrganizationID, principals)
		if err != nil {
			return fmt.Errorf("assistant execution admission: %w", err)
		}
		allowed, err := authz.GrantsAuthorize(grants, authz.Check{Scope: authz.ScopeProjectRead, ResourceID: e.Identity.ProjectID.String(), ResourceKind: "", Dimensions: nil})
		if err != nil {
			return fmt.Errorf("assistant execution admission: %w", err)
		}
		if !allowed {
			return ErrExecutionAdmissionRequired
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit model admission: %w", err)
	}
	return nil
}
