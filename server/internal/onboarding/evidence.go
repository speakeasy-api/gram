package onboarding

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/onboarding/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

// EvidenceWindow is how far back verification looks. Both the next-step loop
// and the product pages' "not set up" state use the same window.
const EvidenceWindow = 30 * 24 * time.Hour

// EvidenceReader reads the ClickHouse side of verification. Tests substitute
// an in-memory implementation.
type EvidenceReader interface {
	// CountHookEvents counts hook events for the projects since the given
	// time; an empty sources list counts every source.
	CountHookEvents(ctx context.Context, projectIDs []string, sources []string, since time.Time) (uint64, error)
	// CountCostRows counts telemetry rows carrying token or cost usage.
	CountCostRows(ctx context.Context, projectIDs []string, since time.Time) (uint64, error)
	// CountGatewayTraffic counts rows dispatched through a toolset or gateway.
	CountGatewayTraffic(ctx context.Context, projectIDs []string, since time.Time) (uint64, error)
	// CountAIDetections counts shadow AI detections reported for the organization.
	CountAIDetections(ctx context.Context, organizationID string, since time.Time) (uint64, error)
}

// evidenceScope is what every evidence read is bounded by: one organization,
// its projects, and the start of the window.
type evidenceScope struct {
	organizationID string
	projectIDs     []string
	since          time.Time
}

func (s *Service) evidenceScope(ctx context.Context, organizationID string) (evidenceScope, error) {
	projects, err := projectsrepo.New(s.db).ListProjectsByOrganization(ctx, organizationID)
	if err != nil {
		return evidenceScope{}, fmt.Errorf("list projects for onboarding evidence: %w", err)
	}
	ids := make([]string, 0, len(projects))
	for _, p := range projects {
		ids = append(ids, p.ID.String())
	}
	return evidenceScope{organizationID: organizationID, projectIDs: ids, since: s.now().Add(-EvidenceWindow)}, nil
}

// checkStep runs a step's evidence read and says what it found.
func (s *Service) checkStep(ctx context.Context, scope evidenceScope, step Step) (bool, string, error) {
	switch step.Evidence {
	case EvidenceHookEvents:
		n, err := s.evidence.CountHookEvents(ctx, scope.projectIDs, step.Sources, scope.since)
		if err != nil {
			return false, "", fmt.Errorf("count hook events: %w", err)
		}
		return n > 0, found(n, "hook event", "from "+strings.Join(step.Sources, ", ")), nil
	case EvidenceCostRows:
		n, err := s.evidence.CountCostRows(ctx, scope.projectIDs, scope.since)
		if err != nil {
			return false, "", fmt.Errorf("count cost rows: %w", err)
		}
		return n > 0, found(n, "usage row", "with tokens or cost"), nil
	case EvidenceGatewayTraffic:
		n, err := s.evidence.CountGatewayTraffic(ctx, scope.projectIDs, scope.since)
		if err != nil {
			return false, "", fmt.Errorf("count gateway traffic: %w", err)
		}
		return n > 0, found(n, "tool call", "through a toolset or gateway"), nil
	case EvidenceAIDetections:
		n, err := s.evidence.CountAIDetections(ctx, scope.organizationID, scope.since)
		if err != nil {
			return false, "", fmt.Errorf("count ai detections: %w", err)
		}
		return n > 0, found(n, "shadow AI detection", "from device agents"), nil
	case EvidenceActivePolicy:
		n, err := repo.New(s.db).CountActiveRiskPolicies(ctx, scope.organizationID)
		if err != nil {
			return false, "", fmt.Errorf("count active risk policies: %w", err)
		}
		if n > 0 {
			return true, fmt.Sprintf("%d enabled risk policies", n), nil
		}
		return false, "No enabled risk policy yet", nil
	case EvidencePluginAssignment:
		n, err := repo.New(s.db).CountPluginAssignments(ctx, scope.organizationID)
		if err != nil {
			return false, "", fmt.Errorf("count plugin assignments: %w", err)
		}
		if n > 0 {
			return true, fmt.Sprintf("%d plugin assignments", n), nil
		}
		return false, "No plugin assigned to an audience yet", nil
	default:
		return false, "", fmt.Errorf("unknown evidence kind %q", step.Evidence)
	}
}

// checkUseCase decides whether the use case is covered. sources narrows the
// observability read to the selected products; nil means any source.
func (s *Service) checkUseCase(ctx context.Context, scope evidenceScope, useCase UseCase, sources []string) (bool, error) {
	switch useCase {
	case UseCaseObservability:
		n, err := s.evidence.CountHookEvents(ctx, scope.projectIDs, sources, scope.since)
		if err != nil {
			return false, fmt.Errorf("count hook events: %w", err)
		}
		return n > 0, nil
	case UseCaseCostTracking:
		n, err := s.evidence.CountCostRows(ctx, scope.projectIDs, scope.since)
		if err != nil {
			return false, fmt.Errorf("count cost rows: %w", err)
		}
		return n > 0, nil
	case UseCaseSecurity:
		policies, err := repo.New(s.db).CountActiveRiskPolicies(ctx, scope.organizationID)
		if err != nil {
			return false, fmt.Errorf("count active risk policies: %w", err)
		}
		if policies == 0 {
			return false, nil
		}
		findings, err := repo.New(s.db).CountRiskFindingsSince(ctx, repo.CountRiskFindingsSinceParams{
			OrganizationID: scope.organizationID,
			Since:          pgtype.Timestamptz{Time: scope.since, InfinityModifier: pgtype.Finite, Valid: true},
		})
		if err != nil {
			return false, fmt.Errorf("count risk findings: %w", err)
		}
		if findings > 0 {
			return true, nil
		}
		detections, err := s.evidence.CountAIDetections(ctx, scope.organizationID, scope.since)
		if err != nil {
			return false, fmt.Errorf("count ai detections: %w", err)
		}
		return detections > 0, nil
	case UseCaseMCPGateway:
		traffic, err := s.evidence.CountGatewayTraffic(ctx, scope.projectIDs, scope.since)
		if err != nil {
			return false, fmt.Errorf("count gateway traffic: %w", err)
		}
		if traffic > 0 {
			return true, nil
		}
		assignments, err := repo.New(s.db).CountPluginAssignments(ctx, scope.organizationID)
		if err != nil {
			return false, fmt.Errorf("count plugin assignments: %w", err)
		}
		return assignments > 0, nil
	default:
		return false, fmt.Errorf("unknown use case %q", useCase)
	}
}

func found(n uint64, noun, qualifier string) string {
	if n == 0 {
		return fmt.Sprintf("No %s %s in the last 30 days yet", noun, qualifier)
	}
	if n == 1 {
		return fmt.Sprintf("1 %s %s in the last 30 days", noun, qualifier)
	}
	return fmt.Sprintf("%d %ss %s in the last 30 days", n, noun, qualifier)
}
