//nolint:exhaustruct // Readiness projections intentionally omit documented optional fields.
package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

const (
	ForcedReadinessProbeLimit      = "platform-mcp-forced-readiness-probe"
	ForcedReadinessProbesPerMinute = 3
)

var (
	ErrReadinessRateLimited          = errors.New("platform mcp readiness probe rate limited")
	ErrReadinessRegistrationNotFound = errors.New("platform mcp readiness registration not found")
)

type ReadinessService struct {
	store        *RegistrationStore
	gate         CatalogRegistrationGateChecker
	adapters     *ProviderAdapters
	generic      CatalogReadinessProber
	forceLimiter Limiter
	repairBudget OperationBudget
	telemetry    LifecycleTelemetry
}

func NewReadinessService(store *RegistrationStore, gate CatalogRegistrationGateChecker, adapters *ProviderAdapters, forceLimiter Limiter, repairBudget OperationBudget, generic ...CatalogReadinessProber) *ReadinessService {
	var genericProber CatalogReadinessProber
	if len(generic) > 0 {
		genericProber = generic[0]
	}
	return &ReadinessService{
		store:        store,
		gate:         gate,
		adapters:     adapters,
		generic:      genericProber,
		forceLimiter: forceLimiter,
		repairBudget: repairBudget,
		telemetry:    noopLifecycleTelemetry{},
	}
}

func (s *ReadinessService) WithTelemetry(telemetry LifecycleTelemetry) *ReadinessService {
	if s != nil && telemetry != nil {
		s.telemetry = telemetry
	}
	return s
}

// CurrentReadiness loads existing, generation-bound readiness without probing a
// provider or consuming a repair budget. It is safe for the dashboard's
// authoritative projection to call while resuming after a reload.
func (s *ReadinessService) CurrentReadiness(ctx context.Context, principal Principal, projectSlug, registrationID string) (ResolvedProject, Readiness, bool, error) {
	return s.getReadiness(ctx, principal, projectSlug, registrationID, false, false)
}

func (s *ReadinessService) GetReadiness(ctx context.Context, principal Principal, projectSlug, registrationID string, force bool) (ResolvedProject, Readiness, bool, error) {
	return s.getReadiness(ctx, principal, projectSlug, registrationID, force, true)
}

func (s *ReadinessService) getReadiness(ctx context.Context, principal Principal, projectSlug, registrationID string, force, consumeBudget bool) (ResolvedProject, Readiness, bool, error) {
	if s == nil || s.store == nil || s.gate == nil || s.adapters == nil || (consumeBudget && !s.repairBudget.valid()) || projectSlug == "" || registrationID == "" {
		return ResolvedProject{}, Readiness{}, false, ErrUnavailable
	}
	if consumeBudget {
		if err := s.repairBudget.Allow(ctx, principal); err != nil {
			return ResolvedProject{}, Readiness{}, false, err
		}
	}
	project, err := s.store.ResolveProject(ctx, principal.OrganizationID, projectSlug)
	if err != nil {
		return ResolvedProject{}, Readiness{}, false, fmt.Errorf("resolve platform mcp readiness project: %w", err)
	}
	enabled, err := s.gate.Enabled(ctx, principal.OrganizationID, project.Slug)
	if err != nil {
		return ResolvedProject{}, Readiness{}, false, fmt.Errorf("check platform mcp readiness gate: %w", err)
	}
	if !enabled {
		return ResolvedProject{}, Readiness{}, false, ErrRegistrationUnavailable
	}
	parsedRegistrationID, err := uuid.Parse(registrationID)
	if err != nil {
		return ResolvedProject{}, Readiness{}, false, ErrReadinessInvalid
	}
	if _, err := lifecycleRegistration(ctx, platformrepo.New(s.store.db), principal, project.ID, parsedRegistrationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ResolvedProject{}, Readiness{}, false, ErrReadinessRegistrationNotFound
		}
		return ResolvedProject{}, Readiness{}, false, fmt.Errorf("resolve platform mcp readiness registration: %w", err)
	}

	if !force {
		readiness, found, err := s.store.GetProviderReadiness(ctx, principal, project.ID, parsedRegistrationID)
		return project, readiness, found, err
	}

	if s.forceLimiter == nil {
		return ResolvedProject{}, Readiness{}, false, ErrUnavailable
	}
	limitKey := principal.OrganizationID + ":" + parsedRegistrationID.String()
	decision, err := s.forceLimiter.Allow(ctx, limitKey)
	if err != nil {
		return ResolvedProject{}, Readiness{}, false, fmt.Errorf("limit platform mcp forced readiness probe: %w", ErrOperationBudgetUnavailable)
	}
	if !decision.Allowed {
		return ResolvedProject{}, Readiness{}, false, ErrReadinessRateLimited
	}
	readiness, err := s.store.ProbeProviderReadiness(ctx, principal, project.ID, parsedRegistrationID, s.adapters, s.generic)
	if err != nil {
		s.telemetry.Record(ctx, LifecycleEvent{Operation: "readiness", Phase: "forced_probe", Outcome: lifecycleOutcome(err), State: ""})
		return ResolvedProject{}, Readiness{}, false, err
	}
	s.telemetry.Record(ctx, LifecycleEvent{Operation: "readiness", Phase: "forced_probe", Outcome: "succeeded", State: readiness.State})
	return project, readiness, true, nil
}

func (s *ReadinessService) GetRepairPlan(ctx context.Context, principal Principal, projectSlug, registrationID string) (ResolvedProject, Readiness, bool, error) {
	project, readiness, found, err := s.GetReadiness(ctx, principal, projectSlug, registrationID, false)
	if err != nil || found {
		return project, readiness, found, err
	}
	return project, normalizedReadiness(readiness, false), false, nil
}

type RepairAction struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
}

func repairActions(state ReadinessState) []RepairAction {
	switch state {
	case ReadinessReady:
		return []RepairAction{}
	case ReadinessNeedsProviderSetup, ReadinessNeedsGramAuthorization, ReadinessAuthFailed, ReadinessUnauthorized:
		return []RepairAction{{Kind: "continue_dashboard_setup", Label: "Complete the secure dashboard source and authentication setup"}}
	case ReadinessNeedsConfiguration:
		return []RepairAction{{Kind: "continue_dashboard_setup", Label: "Complete the required dashboard source configuration"}}
	case ReadinessUnreachable, ReadinessUnsupported, ReadinessDegraded:
		return []RepairAction{{Kind: "retry_readiness", Label: "Retry the authenticated readiness check"}}
	case ReadinessGuideUnavailable:
		return []RepairAction{{Kind: "contact_support", Label: "Ask an administrator to restore the reviewed setup guide"}}
	default:
		return []RepairAction{{Kind: "retry_readiness", Label: "Retry the authenticated readiness check"}}
	}
}

func normalizedReadiness(readiness Readiness, found bool) Readiness {
	if found {
		return readiness
	}
	return Readiness{
		State:        ReadinessDegraded,
		EvidenceCode: "readiness_unavailable",
	}
}

func readinessFreshness(readiness Readiness, found bool) string {
	if !found {
		return "unavailable"
	}
	if readiness.Fresh {
		return "fresh"
	}
	return "stale"
}

func readinessTimestamp(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
