package admission

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/directory"
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

// MissingAudienceCounts summarizes uncovered desired principals without exposing identities.
type MissingAudienceCounts struct {
	Everyone   int `json:"everyone"`
	Roles      int `json:"roles"`
	Groups     int `json:"groups"`
	Attributes int `json:"attributes"`
	Users      int `json:"users"`
}

// Verdict is the bounded result of evaluating one target and complete desired audience.
type Verdict struct {
	State                 State
	MissingAudienceCounts MissingAudienceCounts
	MissingPrincipalURNs  []string `json:"-"`
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
		return Verdict{State: StateEmptyAudience, MissingAudienceCounts: MissingAudienceCounts{Everyone: 0, Roles: 0, Groups: 0, Attributes: 0, Users: 0}, MissingPrincipalURNs: nil}, nil
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
		return Verdict{State: StateNotRequired, MissingAudienceCounts: MissingAudienceCounts{Everyone: 0, Roles: 0, Groups: 0, Attributes: 0, Users: 0}, MissingPrincipalURNs: nil}, nil
	}

	decision, err := approvalrepo.New(tx).GetStandingServerDecisionForAdmission(ctx, approvalrepo.GetStandingServerDecisionForAdmissionParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		TargetKey:      canonicalURL,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Evaluate(desiredPrincipalURNs, Decision{
			Decision:             "",
			GrantedPrincipalURNs: nil,
		})
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
		return Verdict{State: StateEmptyAudience, MissingAudienceCounts: MissingAudienceCounts{Everyone: 0, Roles: 0, Groups: 0, Attributes: 0, Users: 0}, MissingPrincipalURNs: nil}, nil
	}
	canonicalDesired := make([]string, 0, len(desiredPrincipalURNs))
	for _, raw := range desiredPrincipalURNs {
		principal, err := canonicalPrincipal(raw)
		if err != nil {
			return Verdict{}, fmt.Errorf("parse desired principal: %w", err)
		}
		canonicalDesired = append(canonicalDesired, principal)
	}
	switch decision.Decision {
	case "denied", "":
		return Verdict{State: StateApprovalRequired, MissingAudienceCounts: CountMissingAudience(canonicalDesired), MissingPrincipalURNs: canonicalDesired}, nil
	case "approved":
	default:
		return Verdict{}, fmt.Errorf("unknown standing decision %q", decision.Decision)
	}

	granted := decision.GrantedPrincipalURNs
	if len(granted) == 0 {
		granted = []string{authz.AllUsersPrincipal().String()}
	}
	approved := make(map[string]struct{}, len(granted))
	everyone := false
	for _, raw := range granted {
		principal, err := canonicalPrincipal(raw)
		if err != nil {
			return Verdict{}, fmt.Errorf("parse approved principal: %w", err)
		}
		if principal == authz.AllUsersPrincipal().String() {
			everyone = true
		}
		approved[principal] = struct{}{}
	}

	missing := make([]string, 0, len(canonicalDesired))
	for _, principal := range canonicalDesired {
		if everyone {
			continue
		}
		if _, ok := approved[principal]; !ok {
			missing = append(missing, principal)
		}
	}
	if len(missing) > 0 {
		return Verdict{State: StateApprovalRequired, MissingAudienceCounts: CountMissingAudience(missing), MissingPrincipalURNs: append([]string{}, missing...)}, nil
	}
	return Verdict{State: StateCovered, MissingAudienceCounts: MissingAudienceCounts{Everyone: 0, Roles: 0, Groups: 0, Attributes: 0, Users: 0}, MissingPrincipalURNs: nil}, nil
}

// CountMissingAudience summarizes principal kinds for a bounded public projection.
func CountMissingAudience(principals []string) MissingAudienceCounts {
	result := MissingAudienceCounts{Everyone: 0, Roles: 0, Groups: 0, Attributes: 0, Users: 0}
	seen := make(map[string]struct{}, len(principals))
	for _, principal := range principals {
		canonical, err := canonicalPrincipal(principal)
		if err != nil {
			continue
		}
		if _, duplicate := seen[canonical]; duplicate {
			continue
		}
		seen[canonical] = struct{}{}
		switch {
		case canonical == authz.AllUsersPrincipal().String():
			result.Everyone++
		case directory.IsGroupPrincipal(canonical):
			result.Groups++
		case directory.IsAttributePrincipal(canonical):
			result.Attributes++
		default:
			parsed, err := urn.ParsePrincipal(canonical)
			if err != nil {
				continue
			}
			switch parsed.Type {
			case urn.PrincipalTypeRole:
				result.Roles++
			case urn.PrincipalTypeUser:
				result.Users++
			default:
			}
		}
	}
	return result
}

func canonicalPrincipal(raw string) (string, error) {
	switch {
	case raw == urn.PrincipalWildcard:
		return authz.AllUsersPrincipal().String(), nil
	case directory.IsGroupPrincipal(raw):
		id, err := directory.ParseGroupPrincipal(raw)
		if err != nil {
			return "", fmt.Errorf("parse directory group principal: %w", err)
		}
		return directory.GroupPrincipal(id), nil
	case directory.IsAttributePrincipal(raw):
		attribute, err := directory.ParseAttributePrincipal(raw)
		if err != nil {
			return "", fmt.Errorf("parse directory attribute principal: %w", err)
		}
		return directory.AttributePrincipal(attribute.Key, attribute.Value), nil
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
