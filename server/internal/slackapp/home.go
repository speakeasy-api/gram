package slackapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"
	slackrepo "github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
)

// WithDashboardURL sets the Speakeasy dashboard URL the app's Home tab links
// to. Without it the Home tab is not published.
func (e *Events) WithDashboardURL(dashboardURL string) *Events {
	e.dashboardURL = dashboardURL
	return e
}

// WithHome sets where the Home tab's organization data comes from. Without
// it the Home tab only shows the static welcome view.
func (e *Events) WithHome(home *HomeSource) *Events {
	e.home = home
	return e
}

// publishHome answers app_home_opened for the Home tab with views.publish.
// Slack keeps the Home tab spinning until the app publishes a view for the
// user. Best-effort: a failure only costs the Home tab content.
func (e *Events) publishHome(ctx context.Context, summary triggers.SlackEventSummary, body []byte) {
	if e.poster == nil || e.dashboardURL == "" || summary.UserID == "" {
		return
	}
	// Slack fires app_home_opened for the Messages tab too; tab tells them
	// apart. See https://docs.slack.dev/reference/events/app_home_opened.
	var req struct {
		Event struct {
			Tab string `json:"tab"`
		} `json:"event"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Event.Tab != "home" {
		return
	}
	if e.home != nil {
		e.refreshHome(ctx, summary.TeamID, summary.UserID, "", "")
		return
	}

	workspaces, err := e.router.RoutingWorkspacesForTeam(ctx, summary.TeamID)
	if err != nil {
		if !errors.Is(err, slackdirectoryconnections.ErrNoRoutingWorkspace) {
			e.logger.WarnContext(ctx, "find workspace for Speakeasy Slack app home", attr.SlogError(err))
		}
		return
	}
	if len(workspaces) == 0 || workspaces[0].Token == "" {
		return
	}
	e.publishView(ctx, summary.UserID, workspaces[0].Token, triggers.SlackAppHomeView(e.dashboardURL))
}

// homeViewer is the Slack user a Home tab is published for. userID is the
// linked Speakeasy user, an active member of workspace's organization, and
// is empty when the Slack user is not linked to one.
type homeViewer struct {
	workspace slackdirectoryconnections.RoutingWorkspace
	userID    string
}

// resolveHomeViewer finds whose Home a Slack user sees: the first routing
// workspace whose organization links them to an active member. organizationID,
// when set, comes from a signed Home payload and pins the lookup to that
// organization, so a click never resolves to another tenant; it is still
// checked against the workspace's linkage and membership, never trusted
// alone. It reports false when no workspace can publish to the user.
func (e *Events) resolveHomeViewer(ctx context.Context, teamID, slackUserID, organizationID string) (homeViewer, bool, error) {
	var none homeViewer
	workspaces, err := e.router.RoutingWorkspacesForTeam(ctx, teamID)
	if errors.Is(err, slackdirectoryconnections.ErrNoRoutingWorkspace) {
		return none, false, nil
	}
	if err != nil {
		return none, false, fmt.Errorf("find routing workspace: %w", err)
	}
	candidates := make([]slackdirectoryconnections.RoutingWorkspace, 0, len(workspaces))
	for _, workspace := range workspaces {
		if workspace.Token == "" || (organizationID != "" && workspace.OrganizationID != organizationID) {
			continue
		}
		candidates = append(candidates, workspace)
	}
	if len(candidates) == 0 {
		return none, false, nil
	}
	for _, workspace := range candidates {
		userID, err := e.resolveSender(ctx, slackrepo.ResolveSlackMappingUserParams{
			OrganizationID: workspace.OrganizationID,
			SlackTeamID:    teamID,
			SlackUserID:    slackUserID,
		})
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && userID == "") {
			continue
		}
		if err != nil {
			return none, false, fmt.Errorf("resolve Slack user: %w", err)
		}
		member, err := e.home.isMember(ctx, workspace.OrganizationID, userID)
		if err != nil {
			return none, false, err
		}
		if member {
			return homeViewer{workspace: workspace, userID: userID}, true, nil
		}
	}
	return homeViewer{workspace: candidates[0], userID: ""}, true, nil
}

// refreshHome publishes the Home tab for a Slack user. organizationID pins
// the organization as resolveHomeViewer describes; notice is shown at the top.
func (e *Events) refreshHome(ctx context.Context, teamID, slackUserID, organizationID, notice string) {
	viewer, ok, err := e.resolveHomeViewer(ctx, teamID, slackUserID, organizationID)
	if err != nil {
		e.logger.WarnContext(ctx, "resolve Speakeasy Slack app home viewer", attr.SlogSlackTeamID(teamID), attr.SlogError(err))
		return
	}
	if !ok {
		return
	}
	data := HomeData{DashboardURL: e.dashboardURL} //nolint:exhaustruct // an unlinked viewer sees no organization data
	if viewer.userID != "" {
		data = e.home.Load(ctx, viewer.workspace.OrganizationID, viewer.userID, e.dashboardURL)
	}
	data.Notice = notice
	view := HomeView(data)
	if viewer.userID != "" {
		// Clicks on the published view name the organization it was built
		// for; the click handler re-resolves and re-checks it.
		view["private_metadata"] = viewer.workspace.OrganizationID
	}
	e.publishView(ctx, slackUserID, viewer.workspace.Token, view)
}

func (e *Events) publishView(ctx context.Context, slackUserID, token string, view map[string]any) {
	encoded, err := json.Marshal(view)
	if err != nil {
		return
	}
	payload := map[string]any{"user_id": slackUserID, "view": string(encoded)}
	if _, err := e.poster.CallWithToken(ctx, "views.publish", payload, token); err != nil {
		e.logger.WarnContext(ctx, "publish Speakeasy Slack app home", attr.SlogError(err))
	}
}
