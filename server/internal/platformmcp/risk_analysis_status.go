package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/analysisstatus"
)

// ErrRiskFeatureNotEnabled marks a risk read whose capability is switched off
// for the caller's organization. It is distinct from ErrUnavailable, which
// means the deployment cannot answer right now.
var ErrRiskFeatureNotEnabled = errors.New("platform mcp risk capability not enabled")

// riskAnalysisWakeDelay is how long after new chat traffic the Watchdog
// analysis is expected to start. It is the same value the server hands the
// throttled signaler, so the explanation cannot drift from the wiring.
const riskAnalysisWakeDelay = analysisstatus.SignalCooldown

// RiskAnalysisStatusService answers "when did the Watchdog analysis last run"
// for one project. The analysis is event-driven rather than scheduled, so the
// answer is the state of its most recent run.
type RiskAnalysisStatusService struct {
	logger        *slog.Logger
	describer     analysisstatus.Describer
	flags         feature.Provider
	organizations OrganizationSlugResolver
	projects      riskProjectResolver
	now           func() time.Time
}

// NewRiskAnalysisStatusService returns nil when the analysis run state cannot
// be described in this deployment, so the registrar serves the tool as a stub
// rather than reporting a made-up state.
func NewRiskAnalysisStatusService(logger *slog.Logger, db *pgxpool.Pool, describer analysisstatus.Describer, flags feature.Provider, organizations OrganizationSlugResolver) *RiskAnalysisStatusService {
	if logger == nil || db == nil || describer == nil || organizations == nil {
		return nil
	}
	return &RiskAnalysisStatusService{
		logger:        logger.With(attr.SlogComponent("platformmcp")),
		describer:     describer,
		flags:         flags,
		organizations: organizations,
		projects:      postgresRiskProjectResolver{queries: platformrepo.New(db)},
		now:           time.Now,
	}
}

func (s *RiskAnalysisStatusService) valid() bool {
	return s != nil && s.logger != nil && s.describer != nil && s.organizations != nil && s.projects != nil && s.now != nil
}

type GetRiskAnalysisStatusInput struct {
	ProjectID   string `json:"project_id,omitempty"`
	ProjectSlug string `json:"project_slug,omitempty"`
}

type GetRiskAnalysisStatusOutput struct {
	Project RiskProject `json:"project"`
	// State is one of never, idle or running.
	State string `json:"state"`
	// RunningSince is when the in-flight analysis started. Set only when running.
	RunningSince string `json:"running_since,omitempty"`
	// LastRunStartedAt is when the most recent finished analysis began. Set only when idle.
	LastRunStartedAt string `json:"last_run_started_at,omitempty"`
	// LastRunAt is when the most recent analysis finished. Set only when idle.
	LastRunAt string `json:"last_run_at,omitempty"`
	// LastRunOutcome is how the most recent finished analysis ended. Set only when idle.
	LastRunOutcome string `json:"last_run_outcome,omitempty"`
	// Explanation is one or two plain sentences an administrator can act on.
	Explanation string `json:"explanation"`
}

// Get resolves the project, checks the Watchdog rollout flag for it, and
// describes the analysis run state. Flag lookups that error or come back
// indeterminate fail closed, matching the other risk capabilities.
func (s *RiskAnalysisStatusService) Get(ctx context.Context, principal Principal, input GetRiskAnalysisStatusInput) (GetRiskAnalysisStatusOutput, error) {
	if !s.valid() {
		return GetRiskAnalysisStatusOutput{}, ErrUnavailable
	}
	project, err := s.projects.Resolve(ctx, principal.OrganizationID, input.ProjectID, input.ProjectSlug)
	if err != nil {
		return GetRiskAnalysisStatusOutput{}, fmt.Errorf("resolve risk analysis status project: %w", err)
	}
	organizationSlug, err := s.organizations.OrganizationSlug(ctx, principal.OrganizationID)
	if err != nil {
		return GetRiskAnalysisStatusOutput{}, fmt.Errorf("%w: resolve organization slug for risk analysis status: %w", ErrUnavailable, err)
	}
	if organizationSlug == "" {
		return GetRiskAnalysisStatusOutput{}, fmt.Errorf("%w: organization slug is unavailable", ErrUnavailable)
	}
	evaluation, err := feature.EvaluateFlag(ctx, s.flags, feature.FlagRiskWatchdog, principal.OrganizationID, feature.OrgProjectGroups(organizationSlug, project.Slug))
	if err != nil {
		return GetRiskAnalysisStatusOutput{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if evaluation != feature.EvaluationEnabled {
		return GetRiskAnalysisStatusOutput{}, ErrRiskFeatureNotEnabled
	}
	status, err := s.describer.Describe(ctx, project.ID)
	if err != nil {
		s.logger.WarnContext(ctx, "describe risk analysis status",
			attr.SlogError(err),
			attr.SlogOrganizationID(principal.OrganizationID),
			attr.SlogProjectID(project.ID.String()),
		)
		return GetRiskAnalysisStatusOutput{}, fmt.Errorf("%w: describe risk analysis status: %w", ErrUnavailable, err)
	}
	return riskAnalysisStatusOutput(project, status, s.now()), nil
}

func riskAnalysisStatusOutput(project ResolvedProject, status analysisstatus.Status, now time.Time) GetRiskAnalysisStatusOutput {
	return GetRiskAnalysisStatusOutput{
		Project:          riskProject(project),
		State:            string(status.State),
		RunningSince:     rfc3339OrEmpty(status.RunningSince),
		LastRunStartedAt: rfc3339OrEmpty(status.LastRunStartedAt),
		LastRunAt:        rfc3339OrEmpty(status.LastRunAt),
		LastRunOutcome:   status.LastRunOutcome,
		Explanation:      riskAnalysisExplanation(status, now),
	}
}

// riskAnalysisExplanation describes the run state in product language. It
// never names the mechanism behind the analysis; the administrator needs to
// know whether it ran, when, and what makes it run again.
func riskAnalysisExplanation(status analysisstatus.Status, now time.Time) string {
	wake := "It runs within about " + humanDuration(riskAnalysisWakeDelay) + " of new chat traffic, not on a timer."
	switch status.State {
	case analysisstatus.StateRunning:
		if status.RunningSince == nil {
			return "Analysis is in progress now."
		}
		return "Analysis is in progress now. It started " + humanDuration(now.Sub(*status.RunningSince)) + " ago."
	case analysisstatus.StateIdle:
		finished := status.LastRunAt
		if finished == nil {
			finished = status.LastRunStartedAt
		}
		if status.LastRunOutcome != "" && status.LastRunOutcome != "completed" {
			ended := "The last analysis ended with outcome " + strings.ReplaceAll(status.LastRunOutcome, "_", " ")
			if finished != nil {
				ended += " " + humanDuration(now.Sub(*finished)) + " ago"
			}
			return ended + ". Messages it did not analyze are retried automatically on the next run, which starts within about " + humanDuration(riskAnalysisWakeDelay) + " of new chat traffic."
		}
		if finished == nil {
			return "Analysis has run for this project and is waiting for new chat traffic. " + wake
		}
		return "Analysis last finished " + humanDuration(now.Sub(*finished)) + " ago. " + wake
	case analysisstatus.StateNever:
		return "No analysis has run for this project recently. It starts automatically when chat traffic is captured; check that logging is enabled."
	default:
		return "The analysis state could not be determined. " + wake
	}
}

func rfc3339OrEmpty(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

// humanDuration renders a duration at the coarsest unit that keeps it honest:
// "40 seconds", "5 minutes", "3 hours", "2 days". Sub-second and negative
// values read as "a few seconds" so clock skew never produces "-1 seconds".
func humanDuration(d time.Duration) string {
	if d < time.Second {
		return "a few seconds"
	}
	unit := func(count int64, name string) string {
		if count == 1 {
			return "1 " + name
		}
		return fmt.Sprintf("%d %ss", count, name)
	}
	switch {
	case d < time.Minute:
		return unit(int64(d/time.Second), "second")
	case d < time.Hour:
		return unit(int64(d/time.Minute), "minute")
	case d < 24*time.Hour:
		return unit(int64(d/time.Hour), "hour")
	default:
		return unit(int64(d/(24*time.Hour)), "day")
	}
}
