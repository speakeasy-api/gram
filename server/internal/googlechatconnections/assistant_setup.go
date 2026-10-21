package googlechatconnections

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	assistantgen "github.com/speakeasy-api/gram/server/gen/assistants"
	toolsetgen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/googlechatconnections/repo"
	projectrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

// AssistantSetup attaches hosted Chat tools through the existing management services.
type AssistantSetup struct {
	Assistants assistantgen.Service
	Toolsets   toolsetgen.Service
}

func (s *Service) WithAssistantSetup(setup *AssistantSetup) *Service { s.setup = setup; return s }
func (s *Service) setupAssistant(ctx context.Context, b repo.GoogleChatBinding) error {
	if s.setup == nil {
		return fmt.Errorf("assistant setup is not configured")
	}
	ac, ok := contextvalues.GetAuthContext(ctx)
	if !ok {
		return fmt.Errorf("assistant setup requires an administrator")
	}
	scoped := *ac
	scoped.ProjectID = &b.ProjectID
	project, err := projectrepo.New(s.db).GetProjectByIDAndOrganizationID(ctx, projectrepo.GetProjectByIDAndOrganizationIDParams{ID: b.ProjectID, OrganizationID: b.OrganizationID})
	if err != nil {
		return fmt.Errorf("resolve setup project: %w", err)
	}
	ctx = contextvalues.SetAuthContext(ctx, &scoped)
	slug := project.Slug
	states, err := assistantidentity.States(ctx, s.db, b.ProjectID, []uuid.UUID{b.AssistantID})
	if err != nil {
		return fmt.Errorf("setup assistant: %w", err)
	}
	if states[b.AssistantID].State == assistantidentity.NeverConfigured {
		_, err = s.setup.Assistants.UpgradeAssistantIdentity(ctx, &assistantgen.UpgradeAssistantIdentityPayload{ID: b.AssistantID.String(), AgentID: nil, AgentName: nil, SessionToken: nil, ProjectSlugInput: &slug})
		if err != nil {
			return fmt.Errorf("setup assistant: %w", err)
		}
	}
	name := "google-chat-" + b.ID.String()
	toolset, err := s.setup.Toolsets.GetToolset(ctx, &toolsetgen.GetToolsetPayload{Slug: name, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: &slug})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("read Google Chat toolset: %w", err)
		}
		toolset, err = s.setup.Toolsets.CreateToolset(ctx, &toolsetgen.CreateToolsetPayload{SessionToken: nil, ApikeyToken: nil, Name: name, Description: nil, ToolUrns: []string{"tools:platform:googlechat:send_message", "tools:platform:googlechat:reply_to_thread", "tools:platform:googlechat:get_message", "tools:platform:googlechat:list_messages"}, ResourceUrns: nil, DefaultEnvironmentSlug: nil, Origin: nil, ProjectSlugInput: &slug})
		if err != nil {
			return fmt.Errorf("setup assistant: %w", err)
		}
	}
	assistant, err := s.setup.Assistants.GetAssistant(ctx, &assistantgen.GetAssistantPayload{ID: b.AssistantID.String(), SessionToken: nil, ProjectSlugInput: &slug})
	if err != nil {
		return fmt.Errorf("setup assistant: %w", err)
	}
	refs := assistant.McpServers
	found := false
	for _, r := range refs {
		if r.McpServerSlug == string(toolset.Slug) {
			found = true
		}
	}
	if !found {
		refs = append(refs, &types.AssistantMCPServerRef{McpServerSlug: string(toolset.Slug), EnvironmentSlug: nil, EndpointSlug: nil})
	}
	_, err = s.setup.Assistants.UpdateAssistant(ctx, &assistantgen.UpdateAssistantPayload{ID: b.AssistantID.String(), SessionToken: nil, ProjectSlugInput: &slug, Name: nil, Model: nil, Instructions: nil, Toolsets: nil, McpServers: refs, WarmTTLSeconds: nil, MaxConcurrency: nil, Status: nil})
	if err != nil {
		return fmt.Errorf("setup assistant: %w", err)
	}
	return nil
}
