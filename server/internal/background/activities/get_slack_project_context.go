package activities

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"

	project_repo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/slack/types"
	"github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

type GetSlackProjectContext struct {
	slackClient *client.SlackClient
	toolsetRepo *repo.Queries
	projectRepo *project_repo.Queries
	logger      *slog.Logger
}

type SlackToolsetSummary struct {
	ID            uuid.UUID
	Slug          string
	Description   *string
	NumberOfTools int
	CreatedAt     string
	UpdatedAt     string
}

type SlackProjectContextResponse struct {
	TeamName           string
	OrganizationID     string
	ProjectID          uuid.UUID
	ProjectSlug        string
	DefaultToolsetSlug *string
	Toolsets           []SlackToolsetSummary
}

func NewSlackProjectContextActivity(logger *slog.Logger, db *pgxpool.Pool, client *client.SlackClient) *GetSlackProjectContext {
	return &GetSlackProjectContext{
		slackClient: client,
		toolsetRepo: repo.New(db),
		projectRepo: project_repo.New(db),
		logger:      logger,
	}
}

func (s *GetSlackProjectContext) Do(ctx context.Context, event types.SlackEvent) (*SlackProjectContextResponse, error) {
	authInfo, err := s.slackClient.GetAppAuthInfo(ctx, event.TeamID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error getting app auth info").Log(ctx, s.logger)
	}

	toolsets, err := s.toolsetRepo.ListToolsetsByProject(ctx, authInfo.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error getting asset").Log(ctx, s.logger)
	}

	projects, err := s.projectRepo.ListProjectsByOrganization(ctx, authInfo.OrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error getting asset").Log(ctx, s.logger)
	}

	var projectSlug string
	for _, project := range projects {
		if project.ID == authInfo.ProjectID {
			projectSlug = project.Slug
			break
		}
	}

	toolsetSummaries := make([]SlackToolsetSummary, len(toolsets))
	for i, toolset := range toolsets {
		toolsetSummaries[i] = SlackToolsetSummary{
			ID:            toolset.ID,
			Slug:          toolset.Slug,
			Description:   conv.FromPGText[string](toolset.Description),
			NumberOfTools: len(toolset.HttpToolNames),
			CreatedAt:     toolset.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:     toolset.UpdatedAt.Time.Format(time.RFC3339),
		}
	}

	return &SlackProjectContextResponse{
		TeamName:           authInfo.TeamName,
		OrganizationID:     authInfo.OrganizationID,
		ProjectID:          authInfo.ProjectID,
		ProjectSlug:        projectSlug,
		DefaultToolsetSlug: authInfo.DefaultToolsetSlug,
		Toolsets:           toolsetSummaries,
	}, nil
}
