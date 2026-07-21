package skills

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
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
	logger     *slog.Logger
	descriptor core.ToolDescriptor
}

func NewLoadTool(logger *slog.Logger, db *pgxpool.Pool) *Load {
	return &Load{
		db:     db,
		logger: logger,
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

func (t *Load) Call(ctx context.Context, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
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
	loaded, err := queries.LoadAttachedAssistantSkill(ctx, assistantrepo.LoadAttachedAssistantSkillParams{
		AssistantID: uuid.NullUUID{UUID: principal.AssistantID, Valid: true},
		ProjectID:   *authCtx.ProjectID,
		Name:        input.Name,
	})
	if err == nil {
		if _, err := io.WriteString(wr, loaded.Content); err != nil {
			return fmt.Errorf("write attached skill content: %w", err)
		}
		chatID, parseErr := uuid.Parse(env.GramChatID)
		if parseErr != nil || chatID == uuid.Nil {
			t.logger.WarnContext(ctx, "skipping assistant skill observation: missing or invalid Gram chat ID",
				attr.SlogProjectID(authCtx.ProjectID.String()),
				attr.SlogAssistantID(principal.AssistantID.String()),
				attr.SlogAssistantThreadID(principal.ThreadID.String()),
				attr.SlogChatID(env.GramChatID),
				attr.SlogName(loaded.Name),
			)
			return nil
		}
		writeCtx := context.WithoutCancel(ctx)
		if err := queries.RecordAssistantSkillObservation(writeCtx, assistantrepo.RecordAssistantSkillObservationParams{
			SessionID:      chatID.String(),
			SkillVersionID: loaded.SkillVersionID,
			ProjectID:      *authCtx.ProjectID,
			SkillID:        loaded.SkillID,
		}); err != nil {
			t.logger.ErrorContext(writeCtx, "failed to record assistant skill observation",
				attr.SlogError(err),
				attr.SlogProjectID(authCtx.ProjectID.String()),
				attr.SlogAssistantID(principal.AssistantID.String()),
				attr.SlogAssistantThreadID(principal.ThreadID.String()),
				attr.SlogChatID(chatID.String()),
				attr.SlogName(loaded.Name),
			)
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
