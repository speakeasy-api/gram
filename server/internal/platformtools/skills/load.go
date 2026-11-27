package skills

import (
	"context"
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
	"github.com/speakeasy-api/gram/server/internal/skills/skilltree"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type loadInput struct {
	Name string `json:"name" jsonschema:"Name of the attached skill to load."`
	Path string `json:"path,omitempty" jsonschema:"Optional path of one of the skill's supporting files, relative to the skill directory (for example references/REFERENCE.md), as listed when the skill is loaded. Omit to load SKILL.md."`
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
			SourceSlug:    "skills",
			ThreadContext: core.ThreadContextRequired,
			HandlerName:   "load",
			Name:          platformtools.ToolNameSkillsLoad,
			Description:   "Load the exact SKILL.md content for a skill attached to the current assistant, followed by a list of the skill's supporting files when it has any. Pass path to read one supporting file.",
			InputSchema:   core.BuildInputSchema[loadInput](),
			Variables:     nil,
			Annotations:   core.ReadOnlyAnnotations(),
			Managed:       true,
			OwnerKind:     nil,
			OwnerID:       nil,
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
	principal, ok := contextvalues.GetAssistantToolTarget(ctx)
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
	input.Path = strings.TrimSpace(input.Path)
	if input.Path == skilltree.ManifestPath {
		input.Path = ""
	}

	queries := assistantrepo.New(t.db)
	loaded, err := queries.LoadAttachedAssistantSkill(ctx, assistantrepo.LoadAttachedAssistantSkillParams{
		AssistantID: uuid.NullUUID{UUID: principal.AssistantID, Valid: true},
		ProjectID:   *authCtx.ProjectID,
		Name:        input.Name,
	})
	if err == nil && input.Path != "" {
		return t.writeSupportingFile(ctx, queries, *authCtx.ProjectID, loaded, input.Path, wr)
	}
	if err == nil {
		files, err := queries.ListAssistantSkillVersionFileMetadata(ctx, assistantrepo.ListAssistantSkillVersionFileMetadataParams{
			ProjectID:      *authCtx.ProjectID,
			SkillID:        loaded.SkillID,
			SkillVersionID: loaded.SkillVersionID,
		})
		if err != nil {
			return fmt.Errorf("list attached skill files: %w", err)
		}
		if _, err := io.WriteString(wr, loaded.Content+supportingFileList(files)); err != nil {
			return fmt.Errorf("write attached skill content: %w", err)
		}
		chatID, parseErr := uuid.Parse(env.GramChatID)
		if parseErr != nil || chatID == uuid.Nil {
			t.logger.WarnContext(ctx, "skipping assistant skill observation: missing or invalid Speakeasy chat ID",
				attr.SlogProjectID(authCtx.ProjectID.String()),
				attr.SlogAssistantID(principal.AssistantID.String()),
				attr.SlogAssistantThreadID(principal.ThreadID.String()),
				attr.SlogChatID(env.GramChatID),
				attr.SlogName(loaded.Name),
			)
			return nil
		}
		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		written, err := queries.RecordAssistantSkillObservation(writeCtx, assistantrepo.RecordAssistantSkillObservationParams{
			AssistantID:    principal.AssistantID,
			SessionID:      chatID.String(),
			SkillVersionID: loaded.SkillVersionID,
			ProjectID:      *authCtx.ProjectID,
			SkillID:        loaded.SkillID,
		})
		if err != nil {
			t.logger.ErrorContext(writeCtx, "failed to record assistant skill observation",
				attr.SlogError(err),
				attr.SlogProjectID(authCtx.ProjectID.String()),
				attr.SlogAssistantID(principal.AssistantID.String()),
				attr.SlogAssistantThreadID(principal.ThreadID.String()),
				attr.SlogChatID(chatID.String()),
				attr.SlogName(loaded.Name),
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
					attr.SlogName(loaded.Name),
				)
			}
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

// writeSupportingFile writes one supporting file of an attached skill's
// resolved version. Text files are written verbatim; a binary file is
// described by its metadata, since its bytes are no use to the model as text.
// Reading a supporting file follows a load of the skill, so it records no
// activation.
func (t *Load) writeSupportingFile(ctx context.Context, queries *assistantrepo.Queries, projectID uuid.UUID, loaded assistantrepo.LoadAttachedAssistantSkillRow, filePath string, wr io.Writer) error {
	file, err := queries.GetAssistantSkillVersionFile(ctx, assistantrepo.GetAssistantSkillVersionFileParams{
		ProjectID:      projectID,
		SkillID:        loaded.SkillID,
		SkillVersionID: loaded.SkillVersionID,
		Path:           filePath,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeNotFound, nil, "skill %q has no supporting file %q", loaded.Name, filePath)
	}
	if err != nil {
		return fmt.Errorf("get attached skill file: %w", err)
	}

	content := string(file.Content)
	if !skilltree.IsText(file.ContentType, file.Content) {
		content = fmt.Sprintf("%s is a binary file (%s, %d bytes, sha256 %s); its content cannot be shown as text.", file.Path, file.ContentType, file.SizeBytes, file.Sha256)
	}
	if _, err := io.WriteString(wr, content); err != nil {
		return fmt.Errorf("write attached skill file: %w", err)
	}
	return nil
}

// supportingFileList renders the list appended to a loaded SKILL.md that
// names the skill's supporting files, or "" when it has none, so a skill
// without supporting files loads as exactly its SKILL.md.
func supportingFileList(files []assistantrepo.ListAssistantSkillVersionFileMetadataRow) string {
	if len(files) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n\n---\nThis skill has supporting files. Load one by calling this tool again with its path:\n")
	for _, file := range files {
		fmt.Fprintf(&b, "- %s (%s, %d bytes)\n", file.Path, file.ContentType, file.SizeBytes)
	}
	return b.String()
}
