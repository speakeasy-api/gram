package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/skills/efficacy"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type loadInput struct {
	Name string `json:"name" jsonschema:"Name of the attached skill to load."`
}

type turnSkillPayload struct {
	SkillSetSnapshot struct {
		Skills []struct {
			SkillID           uuid.UUID `json:"skill_id"`
			Name              string    `json:"name"`
			ResolvedVersionID uuid.UUID `json:"resolved_version_id"`
		} `json:"skills"`
	} `json:"skill_set_snapshot"`
}

// skillEfficacySignalTimeout bounds one wake. A wake is best-effort and always
// follows a durable write, so it must never hold a tool call open on a slow
// coordinator.
const skillEfficacySignalTimeout = time.Second

type Load struct {
	db               *pgxpool.Pool
	logger           *slog.Logger
	efficacySignaler efficacy.Signaler
	descriptor       core.ToolDescriptor
}

func NewLoadTool(logger *slog.Logger, db *pgxpool.Pool, opts ...LoadOption) *Load {
	t := &Load{
		db:               db,
		logger:           logger,
		efficacySignaler: nil,
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

	for _, opt := range opts {
		opt(t)
	}

	return t
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
	attachedSkill, err := queries.LoadAttachedAssistantSkill(ctx, assistantrepo.LoadAttachedAssistantSkillParams{
		AssistantID: uuid.NullUUID{UUID: principal.AssistantID, Valid: true},
		ProjectID:   *authCtx.ProjectID,
		Name:        input.Name,
	})
	loadedSkill := struct {
		ID        uuid.UUID
		VersionID uuid.UUID
		Name      string
		Content   string
	}{}
	switch {
	case err == nil:
		loadedSkill.ID = attachedSkill.SkillID
		loadedSkill.VersionID = attachedSkill.SkillVersionID
		loadedSkill.Name = attachedSkill.Name
		loadedSkill.Content = attachedSkill.Content
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("load attached assistant skill: %w", err)
	default:
		turnPayload, selectedErr := queries.LoadProcessingAssistantTurnPayload(ctx, assistantrepo.LoadProcessingAssistantTurnPayloadParams{
			ProjectID:        *authCtx.ProjectID,
			AssistantID:      principal.AssistantID,
			ThreadID:         principal.ThreadID,
			ProcessingStatus: "processing",
		})
		if selectedErr != nil && !errors.Is(selectedErr, pgx.ErrNoRows) {
			return fmt.Errorf("load processing assistant turn: %w", selectedErr)
		}

		var selectedSkillID, selectedVersionID uuid.UUID
		if selectedErr == nil {
			var payload turnSkillPayload
			if err := json.Unmarshal(turnPayload, &payload); err != nil {
				return fmt.Errorf("decode processing assistant turn: %w", err)
			}
			for _, skill := range payload.SkillSetSnapshot.Skills {
				if skill.Name == input.Name {
					selectedSkillID = skill.SkillID
					selectedVersionID = skill.ResolvedVersionID
					break
				}
			}
		}

		if selectedSkillID != uuid.Nil && selectedVersionID != uuid.Nil {
			selectedSkill, err := queries.LoadAssistantSkillVersion(ctx, assistantrepo.LoadAssistantSkillVersionParams{
				SkillID:        selectedSkillID,
				ProjectID:      *authCtx.ProjectID,
				SkillVersionID: selectedVersionID,
				Name:           input.Name,
			})
			if err != nil {
				return fmt.Errorf("load turn-selected assistant skill version: %w", err)
			}
			loadedSkill.ID = selectedSkill.SkillID
			loadedSkill.VersionID = selectedSkill.SkillVersionID
			loadedSkill.Name = selectedSkill.Name
			loadedSkill.Content = selectedSkill.Content
		} else {
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
	}

	if _, err := io.WriteString(wr, loadedSkill.Content); err != nil {
		return fmt.Errorf("write assistant skill content: %w", err)
	}
	chatID, parseErr := uuid.Parse(env.GramChatID)
	if parseErr != nil || chatID == uuid.Nil {
		t.logger.WarnContext(ctx, "skipping assistant skill observation: missing or invalid Gram chat ID",
			attr.SlogProjectID(authCtx.ProjectID.String()),
			attr.SlogAssistantID(principal.AssistantID.String()),
			attr.SlogAssistantThreadID(principal.ThreadID.String()),
			attr.SlogChatID(env.GramChatID),
			attr.SlogName(loadedSkill.Name),
		)
		return nil
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	written, err := queries.RecordAssistantSkillObservation(writeCtx, assistantrepo.RecordAssistantSkillObservationParams{
		AssistantID:    principal.AssistantID,
		SessionID:      chatID.String(),
		SkillVersionID: loadedSkill.VersionID,
		ProjectID:      *authCtx.ProjectID,
		SkillID:        loadedSkill.ID,
	})
	if err != nil {
		t.logger.ErrorContext(writeCtx, "failed to record assistant skill observation",
			attr.SlogError(err),
			attr.SlogProjectID(authCtx.ProjectID.String()),
			attr.SlogAssistantID(principal.AssistantID.String()),
			attr.SlogAssistantThreadID(principal.ThreadID.String()),
			attr.SlogChatID(chatID.String()),
			attr.SlogName(loadedSkill.Name),
		)
		return nil
	}

	// The activation is durable by here, so the wake is best-effort and
	// detached from the tool call: a caller that walked away must not drop
	// it, and a coordinator that refuses it must not change what the model
	// sees.
	if written > 0 && t.efficacySignaler != nil {
		signalCtx, cancelSignal := context.WithTimeout(context.WithoutCancel(ctx), skillEfficacySignalTimeout)
		defer cancelSignal()
		if err := t.efficacySignaler.Signal(signalCtx, *authCtx.ProjectID); err != nil {
			t.logger.ErrorContext(signalCtx, "signal skill efficacy coordinator from assistant skill load",
				attr.SlogError(err),
				attr.SlogProjectID(authCtx.ProjectID.String()),
				attr.SlogAssistantID(principal.AssistantID.String()),
				attr.SlogChatID(chatID.String()),
				attr.SlogName(loadedSkill.Name),
			)
		}
	}

	return nil
}
