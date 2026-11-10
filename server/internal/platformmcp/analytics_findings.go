package platformmcp

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/telemetry/analytics"
	signalquery "github.com/speakeasy-api/gram/server/internal/telemetry/analytics/signals"
)

// FindSignalSessionsInput selects an explicit project and observed cohort.
type FindSignalSessionsInput struct {
	// ProjectID is the exact project to inspect.
	ProjectID string `json:"project_id"`
	// From is the inclusive source-time boundary.
	From string `json:"from"`
	// To is the exclusive source-time boundary.
	To string `json:"to"`
	// SignalQuery is the shared measured predicate, without group_by.
	SignalQuery signalquery.Request `json:"signal_query"`
	// Peer optionally selects a second predicate for co-occurrence.
	Peer *signalquery.Selection `json:"peer,omitempty"`
	// ComparePrevious asks for an adjacent equal-duration window.
	ComparePrevious bool `json:"compare_previous"`
	// Evidence includes only references permitted by underlying chat access.
	Evidence bool `json:"evidence"`
}

// FindSignalSessionsOutput contains one authorized project's exploratory findings.
type FindSignalSessionsOutput struct {
	// Project is the exact authorized project.
	Project AnalyticsProject `json:"project"`
	// Findings preserves counts, caveats and permitted reference coordinates.
	Findings *analytics.SignalFindings `json:"findings"`
}

func (s *AnalyticsService) FindSignalSessions(ctx context.Context, principal Principal, input FindSignalSessionsInput) (FindSignalSessionsOutput, error) {
	var zero FindSignalSessionsOutput
	project, err := s.authorizeDataset(ctx, principal, input.ProjectID, "signals")
	if err != nil {
		return zero, err
	}
	if err := s.budget.Allow(ctx, principal); err != nil {
		return zero, err
	}
	from, to, err := analytics.ParseWindow(input.From, input.To)
	if err != nil {
		return zero, fmt.Errorf("parse findings window: %w", err)
	}
	if input.Evidence {
		auth, ok := contextvalues.GetAuthContext(ctx)
		if !ok || auth == nil || principal.UserID == "" || auth.UserID != principal.UserID || auth.ActiveOrganizationID != principal.OrganizationID {
			return zero, ErrForbidden
		}
		bound := *auth
		bound.ProjectID, bound.ProjectSlug = &project.ID, &project.Slug
		ctx = contextvalues.SetAuthContext(ctx, &bound)
	}
	findings, err := s.engine.SignalFindings(ctx, analyticsTenant(principal, project), analytics.Window{FromUnixNano: from, ToUnixNano: to}, analytics.SignalFindingsRequest{Query: input.SignalQuery, Peer: input.Peer, ComparePrevious: input.ComparePrevious, Evidence: input.Evidence}, s.conversationAccess)
	if err != nil {
		return zero, fmt.Errorf("find signal sessions: %w", err)
	}
	return FindSignalSessionsOutput{Project: analyticsProject(project), Findings: findings}, nil
}
