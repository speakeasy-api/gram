package admission

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/authz"
	approvalrepo "github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// State is a closed distribution-admission outcome.
type State string

const (
	// StateEmptyAudience permits a configuration that reaches nobody.
	StateEmptyAudience State = "empty_audience"

	// StateNotRequired means the project has no enabled blocking Shadow MCP policy.
	StateNotRequired State = "not_required"

	// StateCovered means one standing approval covers the complete desired audience.
	StateCovered State = "covered"

	// StateApprovalRequired means approval is missing, denied, or narrower than the desired audience.
	StateApprovalRequired State = "approval_required"
)

// Verdict is the bounded result of evaluating one target and complete desired audience.
type Verdict struct {
	State State
}

// Decision is the standing approval state needed by the pure evaluator.
type Decision struct {
	Decision             string
	GrantedPrincipalURNs []string
}

// LockProject acquires the shared transaction-scoped lock for every writer or
// admission reader of a project's Shadow MCP enforcement state.
func LockProject(ctx context.Context, tx pgx.Tx, projectID uuid.UUID) error {
	if tx == nil || projectID == uuid.Nil {
		return errors.New("invalid shadow MCP admission lock")
	}
	if err := approvalrepo.New(tx).LockProjectEnforcementState(ctx, projectID.String()); err != nil {
		return fmt.Errorf("lock shadow MCP admission project: %w", err)
	}
	return nil
}

// Check evaluates a canonical server URL and complete desired assignment set
// from one transaction snapshot. Callers must acquire LockProject first.
func Check(ctx context.Context, tx pgx.Tx, organizationID string, projectID uuid.UUID, canonicalURL string, desiredPrincipalURNs []string) (Verdict, error) {
	if tx == nil || organizationID == "" || projectID == uuid.Nil || canonicalURL == "" {
		return Verdict{}, errors.New("invalid shadow MCP admission check")
	}
	if _, err := projectsrepo.New(tx).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{
		ID:             projectID,
		OrganizationID: organizationID,
	}); err != nil {
		return Verdict{}, fmt.Errorf("validate shadow MCP admission project: %w", err)
	}
	if len(desiredPrincipalURNs) == 0 {
		return Verdict{State: StateEmptyAudience}, nil
	}

	policies, err := riskrepo.New(tx).ListEnabledShadowMCPPoliciesByProject(ctx, projectID)
	if err != nil {
		return Verdict{}, fmt.Errorf("list blocking Shadow MCP policies: %w", err)
	}
	blocking := false
	for _, policy := range policies {
		if policy.OrganizationID != organizationID {
			return Verdict{}, errors.New("shadow MCP policy organization mismatch")
		}
		if policy.Action == "block" {
			blocking = true
		}
	}
	if !blocking {
		return Verdict{State: StateNotRequired}, nil
	}

	decision, err := approvalrepo.New(tx).GetStandingServerDecisionForAdmission(ctx, approvalrepo.GetStandingServerDecisionForAdmissionParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		TargetKey:      canonicalURL,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Verdict{State: StateApprovalRequired}, nil
	}
	if err != nil {
		return Verdict{}, fmt.Errorf("get standing Shadow MCP decision: %w", err)
	}
	return Evaluate(desiredPrincipalURNs, Decision{
		Decision:             decision.Decision,
		GrantedPrincipalURNs: decision.GrantedPrincipalUrns,
	})
}

// Evaluate compares one standing decision with a complete desired audience.
func Evaluate(desiredPrincipalURNs []string, decision Decision) (Verdict, error) {
	if len(desiredPrincipalURNs) == 0 {
		return Verdict{State: StateEmptyAudience}, nil
	}
	switch decision.Decision {
	case "denied", "":
		return Verdict{State: StateApprovalRequired}, nil
	case "approved":
	default:
		return Verdict{}, fmt.Errorf("unknown standing decision %q", decision.Decision)
	}

	granted := decision.GrantedPrincipalURNs
	if len(granted) == 0 {
		granted = []string{authz.AllUsersPrincipal().String()}
	}
	approved := make(map[string]struct{}, len(granted))
	for _, raw := range granted {
		principal, err := canonicalPrincipal(raw)
		if err != nil {
			return Verdict{}, fmt.Errorf("parse approved principal: %w", err)
		}
		if principal == authz.AllUsersPrincipal().String() {
			return Verdict{State: StateCovered}, nil
		}
		approved[principal] = struct{}{}
	}

	for _, raw := range desiredPrincipalURNs {
		principal, err := canonicalPrincipal(raw)
		if err != nil {
			return Verdict{}, fmt.Errorf("parse desired principal: %w", err)
		}
		if _, ok := approved[principal]; !ok {
			return Verdict{State: StateApprovalRequired}, nil
		}
	}
	return Verdict{State: StateCovered}, nil
}

func canonicalPrincipal(raw string) (string, error) {
	if raw == urn.PrincipalWildcard {
		return authz.AllUsersPrincipal().String(), nil
	}
	principal, err := urn.ParsePrincipal(raw)
	if err != nil {
		return "", fmt.Errorf("parse principal: %w", err)
	}
	if principal.IsZero() {
		return "", urn.ErrInvalid
	}
	return principal.String(), nil
}
