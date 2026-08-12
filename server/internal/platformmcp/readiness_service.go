package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const ForcedReadinessProbeLimit = "platform-mcp-forced-readiness-probe"

var ErrReadinessRateLimited = errors.New("platform mcp readiness probe rate limited")

type ReadinessService struct {
	store        *RegistrationStore
	gate         Gate
	adapters     *ProviderAdapters
	forceLimiter Limiter
	repairBudget OperationBudget
	telemetry    LifecycleTelemetry
}

func NewReadinessService(store *RegistrationStore, gate Gate, adapters *ProviderAdapters, forceLimiter Limiter, repairBudget OperationBudget) *ReadinessService {
	return &ReadinessService{
		store:        store,
		gate:         gate,
		adapters:     adapters,
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

func (s *ReadinessService) GetReadiness(ctx context.Context, principal Principal, projectSlug, registrationID string, force bool) (ResolvedProject, Readiness, bool, error) {
	if s == nil || s.store == nil || s.gate == nil || s.adapters == nil || !s.repairBudget.valid() || projectSlug == "" || registrationID == "" {
		return ResolvedProject{}, Readiness{}, false, ErrUnavailable
	}
	parsedRegistrationID, err := uuid.Parse(registrationID)
	if err != nil {
		return ResolvedProject{}, Readiness{}, false, ErrReadinessInvalid
	}
	if err := s.repairBudget.Allow(ctx, principal); err != nil {
		return ResolvedProject{}, Readiness{}, false, err
	}
	project, err := s.store.ResolveProject(ctx, principal.OrganizationID, projectSlug)
	if err != nil {
		return ResolvedProject{}, Readiness{}, false, fmt.Errorf("resolve platform mcp readiness project: %w", err)
	}
	enabled, err := s.gate.Enabled(ctx, principal.OrganizationID)
	if err != nil {
		return ResolvedProject{}, Readiness{}, false, fmt.Errorf("check platform mcp readiness gate: %w", err)
	}
	if !enabled {
		return ResolvedProject{}, Readiness{}, false, ErrRegistrationUnavailable
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
	readiness, err := s.store.ProbeProviderReadiness(ctx, principal, project.ID, parsedRegistrationID, s.adapters)
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
	return project, Readiness{
		ID:                   uuid.Nil,
		ProjectID:            project.ID,
		RegistrationID:       uuid.Nil,
		State:                ReadinessDegraded,
		EvidenceCode:         "",
		CheckedAt:            time.Time{},
		ExpiresAt:            time.Time{},
		ConnectionID:         uuid.Nil,
		ConnectionGeneration: uuid.Nil,
		Fresh:                false,
	}, false, nil
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
		return []RepairAction{{Kind: "start_setup", Label: "Start or repeat the secure provider setup handoff"}}
	case ReadinessNeedsConfiguration:
		return []RepairAction{{Kind: "retry_registration", Label: "Retry the reviewed registration and setup flow"}}
	case ReadinessUnreachable, ReadinessUnsupported, ReadinessDegraded:
		return []RepairAction{{Kind: "retry_readiness", Label: "Retry the authenticated readiness check"}}
	case ReadinessGuideUnavailable:
		return []RepairAction{{Kind: "contact_support", Label: "Ask an administrator to restore the reviewed setup guide"}}
	default:
		return []RepairAction{{Kind: "retry_readiness", Label: "Retry the authenticated readiness check"}}
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
