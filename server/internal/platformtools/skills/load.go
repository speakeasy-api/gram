package skills

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type loadInput struct {
	Name string `json:"name" jsonschema:"Name of the attached skill to load."`
}

type Load struct {
	db         *pgxpool.Pool
	descriptor core.ToolDescriptor
}

func NewLoadTool(db *pgxpool.Pool) *Load {
	return &Load{
		db: db,
		descriptor: core.ToolDescriptor{
			SourceSlug:  "skills",
			HandlerName: "load",
			Name:        platformtools.ToolNameSkillsLoad,
			Description: "Load the exact SKILL.md content for a skill attached to the current assistant.",
			InputSchema: core.BuildInputSchema[loadInput](),
			Variables:   nil,
			Annotations: core.ReadOnlyAnnotations(),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
	}
}

func (t *Load) Descriptor() core.ToolDescriptor {
	return t.descriptor
}

func (t *Load) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	principal, ok := contextvalues.GetAssistantPrincipal(ctx)
	if !ok {
		return oops.E(oops.CodeUnauthorized, nil, "skills tools require an assistant principal")
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.E(oops.CodeUnauthorized, nil, "skills tools require a project auth context")
	}

	var input loadInput
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return oops.E(oops.CodeBadRequest, nil, "name is required")
	}

	queries := assistantrepo.New(t.db)
	content, err := queries.LoadAttachedAssistantSkill(ctx, assistantrepo.LoadAttachedAssistantSkillParams{
		AssistantID: uuid.NullUUID{UUID: principal.AssistantID, Valid: true},
		ProjectID:   *authCtx.ProjectID,
		Name:        input.Name,
	})
	if err == nil {
		if _, err := io.WriteString(wr, content); err != nil {
			return fmt.Errorf("write attached skill content: %w", err)
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("load attached assistant skill: %w", err)
	}

	attached, err := queries.LoadAssistantSkills(ctx, assistantrepo.LoadAssistantSkillsParams{
		AssistantIds: []uuid.UUID{principal.AssistantID},
		ProjectID:    *authCtx.ProjectID,
	})
	if err != nil {
		return fmt.Errorf("check attached assistant skills: %w", err)
	}
	if len(attached) > 0 {
		return oops.E(oops.CodeNotFound, nil, "skill is not attached to this assistant")
	}
	if _, err := io.WriteString(wr, "no skills attached"); err != nil {
		return fmt.Errorf("write empty attached skills result: %w", err)
	}
	return nil
}
