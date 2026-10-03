package assistantidentity

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ExecutionGrant is deliberately not a management or business capability.
func ExecutionGrant(assistant, project uuid.UUID) authz.Grant {
	g := authz.NewGrant(authz.ScopeAssistantExecute, assistant.String())
	g.Selector[authz.SelectorKeyProjectID] = project.String()
	return g
}

// grantExecution is called only for new provisioning or explicit upgrade. A
// current assistant administrator may delegate this exact
// assistant capability; no general chat grant is copied to the agent.
func grantExecution(ctx context.Context, tx pgx.Tx, p ProvisionParams, agent uuid.UUID) (bool, error) {
	if _, err := repo.New(tx).LockDedicatedAgent(ctx, repo.LockDedicatedAgentParams{OrganizationID: p.OrganizationID, ProjectID: uuid.NullUUID{UUID: p.ProjectID, Valid: true}, AgentID: agent}); err != nil {
		return false, fmt.Errorf("lock execution agent: %w", err)
	}
	principals, err := authz.ResolveUserPrincipals(ctx, tx, p.OrganizationID, p.ActorUserID)
	if err != nil {
		return false, fmt.Errorf("resolve execution provisioner: %w", err)
	}
	grants, err := authz.LoadGrants(ctx, tx, p.OrganizationID, principals)
	if err != nil {
		return false, fmt.Errorf("load execution provisioner: %w", err)
	}
	allowed, err := authz.GrantsAuthorize(grants, authz.Check{Scope: authz.ScopeProjectWrite, ResourceID: p.ProjectID.String(), ResourceKind: "", Dimensions: nil})
	if err != nil {
		return false, fmt.Errorf("authorize execution provisioner: %w", err)
	}
	if !allowed {
		return false, ErrExecutionAdmissionRequired
	}

	grant := ExecutionGrant(p.AssistantID, p.ProjectID)
	selector, err := json.Marshal(grant.Selector)
	if err != nil {
		return false, fmt.Errorf("assistant execution admission: %w", err)
	}
	rows, err := accessrepo.New(tx).InsertPrincipalGrantIfAbsent(ctx, accessrepo.InsertPrincipalGrantIfAbsentParams{OrganizationID: p.OrganizationID, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeAgent, agent.String()), Scope: string(grant.Scope), Selectors: selector})
	if err != nil {
		return false, fmt.Errorf("grant assistant execution: %w", err)
	}
	return rows > 0, nil
}

// AdmitModel is the shared dispatch and per-request model-work boundary. It
// reads lifecycle and live agent policy together. The saved ceiling can only
// restrict this policy, never authorize work on its own. No owner policy is
// used for workload execution. Legacy entry points retain their own admission.
// The human branch is the integration point for future ai_access enforcement;
// today human eligibility uses shipped project authorization, not the unmerged
// ai_access feature. Model permission itself belongs to the authorized agent.
func (s *Service) AdmitModel(ctx context.Context, db DB, e Execution) error {
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
	agent, err := runtimepolicy.LoadAgentPolicy(ctx, tx, e.Identity.OrganizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, e.Identity.AgentID.String()))
	if err != nil {
		return fmt.Errorf("load execution policy: %w", err)
	}
	if err := AdmitModelPolicy(e, agent); err != nil {
		return fmt.Errorf("assistant execution admission: %w", err)
	}
	if e.Mode == ExecutionWorkloadHuman {
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

// AdmitModelPolicy is the non-escalating policy half of admission, never a
// replacement for live lifecycle validation by AdmitModel.
func AdmitModelPolicy(e Execution, live []authz.Grant) error {
	if err := e.Check(); err != nil {
		return fmt.Errorf("assistant execution admission: %w", err)
	}
	policy, err := runtimepolicy.DecodeDelegatedPolicy(e.Ceiling.EncodingVersion, e.Ceiling.Policy)
	if err != nil {
		return fmt.Errorf("assistant execution admission: %w", err)
	}
	check := authz.AssistantExecuteCheck(e.Identity.AssistantID.String(), e.Identity.ProjectID.String())
	for _, grants := range [][]authz.Grant{policy.RuntimeGrants(), live} {
		allowed, err := authz.GrantsAuthorize(grants, check)
		if err != nil {
			return fmt.Errorf("assistant execution admission: %w", err)
		}
		if !allowed {
			return ErrExecutionAdmissionRequired
		}
	}
	return nil
}
