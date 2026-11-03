package slackdirectoryconnections

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/slackscopes"
)

// ErrNoRoutingWorkspace means no connected workspace routes the Speakeasy app
// to the project, or the workspace's bot token cannot be used right now.
var ErrNoRoutingWorkspace = errors.New("no Slack workspace answers for this project")

// RoutingWorkspace is a connected workspace whose Speakeasy app mentions and
// direct messages go to a project's managed assistant. The token never leaves
// the process: callers use it for Slack Web API calls only.
type RoutingWorkspace struct {
	// ConnectionID is the organization's connection to the workspace.
	ConnectionID uuid.UUID

	// OrganizationID owns the connection.
	OrganizationID string

	// TeamID is the Slack workspace ID.
	TeamID string

	// ProjectID is the project whose managed assistant answers.
	ProjectID uuid.UUID

	// Token is the workspace's bot token.
	Token string
}

// RoutingWorkspacesForTeam returns every connected workspace for teamID that
// routes the Speakeasy app to a project and can post right now. The same
// workspace can be connected by several organizations, so callers decide what
// to do when more than one answers.
func (s *DirectorySync) RoutingWorkspacesForTeam(ctx context.Context, teamID string) ([]RoutingWorkspace, error) {
	rows, err := repo.New(s.db).ListSlackRoutingConnectionsByTeam(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("list Slack workspaces for team: %w", err)
	}
	workspaces := make([]RoutingWorkspace, 0, len(rows))
	var unavailable error
	for _, row := range rows {
		token, err := s.routingToken(ctx, row)
		if err != nil {
			unavailable = errors.Join(unavailable, err)
			continue
		}
		workspaces = append(workspaces, RoutingWorkspace{ConnectionID: row.ID, OrganizationID: row.OrganizationID, TeamID: row.SlackTeamID, ProjectID: row.DefaultProjectID.UUID, Token: token})
	}
	if len(workspaces) == 0 && unavailable != nil {
		return nil, unavailable
	}
	return workspaces, nil
}

// ProjectBotToken returns the bot token of the workspace that routes the
// Speakeasy app to projectID. It returns ErrNoRoutingWorkspace when there is
// none or its token cannot be used right now.
func (s *DirectorySync) ProjectBotToken(ctx context.Context, organizationID string, projectID uuid.UUID) (string, error) {
	row, err := repo.New(s.db).GetSlackRoutingConnectionByProject(ctx, repo.GetSlackRoutingConnectionByProjectParams{OrganizationID: organizationID, DefaultProjectID: uuid.NullUUID{UUID: projectID, Valid: true}})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoRoutingWorkspace
	}
	if err != nil {
		return "", fmt.Errorf("read Slack workspace for project: %w", err)
	}
	return s.routingToken(ctx, row)
}

// routingToken returns a connection's bot token when it can post, renewing a
// rotating token that is close to expiry.
func (s *DirectorySync) routingToken(ctx context.Context, row repo.SlackDirectoryConnection) (string, error) {
	if !slices.Contains(row.GrantedScopes, slackscopes.ChatWrite) {
		return "", fmt.Errorf("%w: chat:write was not granted", ErrNoRoutingWorkspace)
	}
	tokens, err := usableTokens(s.encryption, row)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrNoRoutingWorkspace, err)
	}
	if tokens.ExpiresAt == nil || time.Until(*tokens.ExpiresAt) > tokenRefreshMargin {
		return tokens.AccessToken, nil
	}
	refreshed, err := s.refreshForMessaging(ctx, row.OrganizationID, row.ID)
	switch {
	case err == nil && refreshed == nil:
		return "", ErrNoRoutingWorkspace
	case err == nil:
		return refreshed.AccessToken, nil
	case time.Until(*tokens.ExpiresAt) <= minTokenLifetime:
		return "", fmt.Errorf("%w: %w", ErrNoRoutingWorkspace, err)
	default:
		// The current token still works; a later call renews it.
		return tokens.AccessToken, nil
	}
}
