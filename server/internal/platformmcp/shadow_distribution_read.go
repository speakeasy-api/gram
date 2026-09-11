package platformmcp

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
)

const (
	DistributionAdmissionNotApplicable  = "not_applicable"
	DistributionAdmissionNotDistributed = "not_distributed"
	DistributionAdmissionEmptyAudience  = "empty_audience"
	DistributionAdmissionNotRequired    = "not_required"
	DistributionAdmissionCovered        = "covered"
	DistributionAdmissionRepairRequired = "repair_required"
	DistributionAdmissionUnavailable    = "unavailable"
)

type DistributionAdmission struct {
	State                 string                          `json:"state"`
	Mode                  string                          `json:"mode,omitempty"`
	MissingAudienceCounts admission.MissingAudienceCounts `json:"missing_audience_counts"`
	CheckedAt             string                          `json:"checked_at"`
	Complete              bool                            `json:"complete"`
}

type distributionAdmissionTarget struct {
	url      string
	audience []string
}

type distributionAdmissionReader interface {
	ForPlugin(context.Context, string, uuid.UUID, uuid.UUID) DistributionAdmission
	ForTarget(context.Context, string, uuid.UUID, string) DistributionAdmission
	NotApplicable(context.Context, string, uuid.UUID) DistributionAdmission
}

type ShadowDistributionReadService struct {
	logger        *slog.Logger
	db            *pgxpool.Pool
	guard         *admission.Guard
	organizations OrganizationSlugResolver
	now           func() time.Time
}

func NewShadowDistributionReadService(logger *slog.Logger, db *pgxpool.Pool, guard *admission.Guard, organizations OrganizationSlugResolver) *ShadowDistributionReadService {
	return &ShadowDistributionReadService{logger: logger, db: db, guard: guard, organizations: organizations, now: time.Now}
}

func (s *ShadowDistributionReadService) valid() bool {
	return s != nil && s.logger != nil && s.db != nil && s.guard != nil && s.organizations != nil && s.now != nil
}

func (s *ShadowDistributionReadService) ForPlugin(ctx context.Context, organizationID string, projectID, pluginID uuid.UUID) DistributionAdmission {
	return s.read(ctx, organizationID, projectID, DistributionAdmissionNotApplicable, func(ctx context.Context, tx pgx.Tx) ([]distributionAdmissionTarget, error) {
		targets, err := platformrepo.New(tx).ListDirectRemoteAdmissionTargetsForPlugin(ctx, platformrepo.ListDirectRemoteAdmissionTargetsForPluginParams{PluginID: pluginID, OrganizationID: organizationID, ProjectID: projectID})
		if err != nil {
			return nil, fmt.Errorf("list plugin distribution targets: %w", err)
		}
		assignments, err := platformrepo.New(tx).ListPlatformMCPPluginAssignments(ctx, platformrepo.ListPlatformMCPPluginAssignmentsParams{PluginID: pluginID, OrganizationID: organizationID, ProjectID: projectID})
		if err != nil {
			return nil, fmt.Errorf("list plugin distribution audience: %w", err)
		}
		result := make([]distributionAdmissionTarget, 0, len(targets))
		for _, target := range targets {
			if !target.RemoteUrl.Valid || target.RemoteUrl.String == "" {
				return nil, fmt.Errorf("plugin distribution target is incomplete")
			}
			canonical, ok := shadowmcp.CanonicalizeInventoryURL(target.RemoteUrl.String)
			if !ok {
				return nil, fmt.Errorf("plugin distribution target URL is invalid")
			}
			result = append(result, distributionAdmissionTarget{url: canonical.CanonicalURL, audience: assignments})
		}
		return result, nil
	})
}

func (s *ShadowDistributionReadService) NotApplicable(ctx context.Context, organizationID string, projectID uuid.UUID) DistributionAdmission {
	return s.read(ctx, organizationID, projectID, DistributionAdmissionNotApplicable, func(context.Context, pgx.Tx) ([]distributionAdmissionTarget, error) {
		return nil, nil
	})
}

func (s *ShadowDistributionReadService) ForTarget(ctx context.Context, organizationID string, projectID uuid.UUID, targetURL string) DistributionAdmission {
	canonical, ok := shadowmcp.CanonicalizeInventoryURL(targetURL)
	if !ok {
		return s.unavailable()
	}
	return s.read(ctx, organizationID, projectID, DistributionAdmissionNotDistributed, func(ctx context.Context, tx pgx.Tx) ([]distributionAdmissionTarget, error) {
		q := platformrepo.New(tx)
		candidates, err := q.ListDirectRemoteAdmissionTargetCandidates(ctx, platformrepo.ListDirectRemoteAdmissionTargetCandidatesParams{OrganizationID: organizationID, ProjectID: projectID})
		if err != nil {
			return nil, fmt.Errorf("list target distribution candidates: %w", err)
		}
		if len(candidates) > 100 {
			return nil, fmt.Errorf("target distribution candidates are incomplete")
		}

		result := make([]distributionAdmissionTarget, 0, len(candidates))
		for _, candidate := range candidates {
			stored, ok := shadowmcp.CanonicalizeInventoryURL(candidate.RemoteUrl)
			if !ok {
				continue
			}
			if stored.CanonicalURL != canonical.CanonicalURL {
				continue
			}
			rows, err := q.ListDirectRemoteAdmissionAudiencesForMCPServer(ctx, platformrepo.ListDirectRemoteAdmissionAudiencesForMCPServerParams{OrganizationID: organizationID, ProjectID: projectID, McpServerID: uuid.NullUUID{UUID: candidate.McpServerID, Valid: true}})
			if err != nil {
				return nil, fmt.Errorf("list target distribution audiences: %w", err)
			}
			audiences := make(map[uuid.UUID][]string)
			for _, row := range rows {
				if !row.PluginID.Valid {
					continue
				}
				if _, exists := audiences[row.PluginID.UUID]; !exists {
					audiences[row.PluginID.UUID] = nil
				}
				if row.PrincipalUrn.Valid {
					audiences[row.PluginID.UUID] = append(audiences[row.PluginID.UUID], row.PrincipalUrn.String)
				}
			}
			for _, audience := range audiences {
				result = append(result, distributionAdmissionTarget{url: canonical.CanonicalURL, audience: audience})
			}
		}
		return result, nil
	})
}

func (s *ShadowDistributionReadService) read(ctx context.Context, organizationID string, projectID uuid.UUID, emptyState string, load func(context.Context, pgx.Tx) ([]distributionAdmissionTarget, error)) DistributionAdmission {
	result := s.unavailable()
	if !s.valid() || organizationID == "" || projectID == uuid.Nil {
		return result
	}
	organizationSlug, err := s.organizations.OrganizationSlug(ctx, organizationID)
	if err != nil || organizationSlug == "" {
		s.warn(ctx, "resolve organization slug", err)
		return result
	}
	rollout, err := s.guard.ResolveProject(ctx, s.db, organizationID, organizationSlug, projectID)
	if err != nil {
		s.warn(ctx, "resolve rollout mode", err)
		return result
	}
	result.Mode = string(rollout.Mode)
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly, DeferrableMode: "", BeginQuery: "", CommitQuery: ""})
	if err != nil {
		s.warn(ctx, "begin snapshot", err)
		return result
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := admission.LockProject(ctx, tx, projectID); err != nil {
		s.warn(ctx, "lock enforcement snapshot", err)
		return result
	}
	targets, err := load(ctx, tx)
	if err != nil {
		s.warn(ctx, "load distribution audience", err)
		return result
	}
	if len(targets) == 0 {
		result.State = emptyState
		result.Complete = true
		return result
	}
	state := admission.StateEmptyAudience
	missingPrincipals := make(map[string]struct{})
	for _, target := range targets {
		verdict, err := admission.Check(ctx, tx, organizationID, projectID, target.url, target.audience)
		if err != nil {
			s.warn(ctx, "evaluate distribution admission", err)
			return result
		}
		state = aggregateAdmissionState(state, verdict.State)
		for _, principal := range verdict.MissingPrincipalURNs {
			missingPrincipals[principal] = struct{}{}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		s.warn(ctx, "commit snapshot", err)
		return result
	}
	result.State = string(state)
	if state == admission.StateApprovalRequired {
		result.State = DistributionAdmissionRepairRequired
	}
	missing := make([]string, 0, len(missingPrincipals))
	for principal := range missingPrincipals {
		missing = append(missing, principal)
	}
	result.MissingAudienceCounts = admission.CountMissingAudience(missing)
	result.Complete = true
	return result
}

func (s *ShadowDistributionReadService) warn(ctx context.Context, operation string, err error) {
	if s == nil || s.logger == nil {
		return
	}
	attrs := []any{attr.SlogReason(operation)}
	if err != nil {
		attrs = append(attrs, attr.SlogError(err))
	}
	s.logger.WarnContext(ctx, "read platform mcp shadow distribution admission", attrs...)
}

func (s *ShadowDistributionReadService) unavailable() DistributionAdmission {
	if s == nil {
		return unavailableDistributionAdmission(nil)
	}
	return unavailableDistributionAdmission(s.now)
}

func unavailableDistributionAdmission(now func() time.Time) DistributionAdmission {
	checkedAt := ""
	if now != nil {
		checkedAt = now().UTC().Format(time.RFC3339Nano)
	}
	return DistributionAdmission{State: DistributionAdmissionUnavailable, Mode: "", MissingAudienceCounts: admission.MissingAudienceCounts{Everyone: 0, Roles: 0, Groups: 0, Attributes: 0, Users: 0}, CheckedAt: checkedAt, Complete: false}
}

func aggregateAdmissionState(current, next admission.State) admission.State {
	rank := func(state admission.State) int {
		switch state {
		case admission.StateApprovalRequired:
			return 4
		case admission.StateCovered:
			return 3
		case admission.StateNotRequired:
			return 2
		case admission.StateEmptyAudience:
			return 1
		default:
			return 0
		}
	}
	if rank(next) > rank(current) {
		return next
	}
	return current
}
